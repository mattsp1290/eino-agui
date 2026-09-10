package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
)

type BlockContextResolver interface {
	ResolveBlock(index int, kind schema.ContentBlockType) (convert.AgenticBlockContext, error)
}

type TransientSink interface {
	Emit(events.Event) error
	Detach(error)
}

type AgenticStreamIdentity struct {
	SessionID string
	RunID     string
	TurnID    string
	MessageID string
	AttemptID string
	AgentPath []convert.AgentPathSegment
}

type AgenticTerminal string

const (
	AgenticTerminalEOF       AgenticTerminal = "eof"
	AgenticTerminalError     AgenticTerminal = "error"
	AgenticTerminalCancelled AgenticTerminal = "cancelled"
)

type AgenticResult struct {
	Assistant          *schema.AgenticMessage
	PublicProjection   *convert.AgenticProjection
	DeliveredTransient []events.Event
	ObserverErr        error
	Usage              *schema.TokenUsage
	SeenBlockIndices   []int
	Partial            bool
	Terminal           AgenticTerminal
}

type AgenticOption func(*agenticConfig)
type agenticConfig struct {
	modelOptions []model.Option
	sink         TransientSink
	limits       convert.ProjectionLimits
	expectedRole schema.AgenticRoleType
}

func WithAgenticModelOptions(options ...model.Option) AgenticOption {
	copyOptions := append([]model.Option(nil), options...)
	return func(config *agenticConfig) { config.modelOptions = append([]model.Option(nil), copyOptions...) }
}
func WithTransientSink(sink TransientSink) AgenticOption {
	return func(config *agenticConfig) { config.sink = sink }
}
func WithProjectionLimits(limits convert.ProjectionLimits) AgenticOption {
	return func(config *agenticConfig) { config.limits = limits }
}

func StreamAgenticTurn(ctx context.Context, am model.AgenticModel, messages []*schema.AgenticMessage, ids AgenticStreamIdentity, blocks BlockContextResolver, opts ...AgenticOption) (*AgenticResult, error) {
	if am == nil {
		return nil, errors.New("agentic model is required")
	}
	if blocks == nil {
		return nil, errors.New("block context resolver is required")
	}
	config := agenticConfig{limits: convert.DefaultProjectionLimits(), expectedRole: schema.AgenticRoleTypeAssistant}
	for _, option := range opts {
		if option != nil {
			option(&config)
		}
	}
	// Validate identity and limits without opening the source. An empty assistant
	// message is sufficient because projection performs all shared validation.
	if err := validateAgenticStreamIdentity(ids, config.limits); err != nil {
		return nil, err
	}
	input := append([]*schema.AgenticMessage(nil), messages...)
	modelOptions := append([]model.Option(nil), config.modelOptions...)
	reader, err := am.Stream(ctx, input, modelOptions...)
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, errors.New("agentic model returned a nil stream")
	}
	return drainAgenticReader(ctx, reader, ids, blocks, config)
}

type blockLedger struct {
	kind    schema.ContentBlockType
	context convert.AgenticBlockContext
	callID  string
	name    string
}
type agenticDrain struct {
	result        *AgenticResult
	chunks        []*schema.AgenticMessage
	base          convert.AgenticIdentityV1
	resolver      BlockContextResolver
	config        agenticConfig
	indexed       map[int]blockLedger
	mode          string
	nextUnindexed int
	detached      bool
}

func drainAgenticReader(ctx context.Context, reader *schema.StreamReader[*schema.AgenticMessage], ids AgenticStreamIdentity, resolver BlockContextResolver, config agenticConfig) (*AgenticResult, error) {
	state := &agenticDrain{result: &AgenticResult{}, base: streamIdentity(ids), resolver: resolver, config: config, indexed: map[int]blockLedger{}}
	var closeOnce sync.Once
	closeReader := func() { closeOnce.Do(reader.Close) }
	defer closeReader()
	for {
		chunk, recvErr := receiveAgenticChunk(ctx, reader, closeReader)
		if errors.Is(recvErr, io.EOF) {
			return state.finish(nil)
		}
		if recvErr != nil {
			return state.finish(recvErr)
		}
		if err := state.apply(chunk); err != nil {
			return state.finish(err)
		}
	}
}

type agenticReceive struct {
	chunk *schema.AgenticMessage
	err   error
}

func receiveAgenticChunk(ctx context.Context, reader *schema.StreamReader[*schema.AgenticMessage], closeReader func()) (*schema.AgenticMessage, error) {
	received := make(chan agenticReceive, 1)
	go func() {
		chunk, err := reader.Recv()
		received <- agenticReceive{chunk: chunk, err: err}
	}()
	select {
	case item := <-received:
		return item.chunk, item.err
	case <-ctx.Done():
		closeReader()
		<-received
		return nil, ctx.Err()
	}
}

func (s *agenticDrain) apply(chunk *schema.AgenticMessage) error {
	if chunk == nil {
		return errors.New("agentic stream returned a nil chunk")
	}
	if s.config.expectedRole != "" && chunk.Role != s.config.expectedRole {
		return errors.New("agentic stream chunk role does not match expected role")
	}
	if len(s.chunks) >= s.config.limits.MaxStreamChunks {
		return errors.New("agentic stream chunk limit exceeded")
	}
	s.chunks = append(s.chunks, chunk)
	if chunk.ResponseMeta != nil && chunk.ResponseMeta.TokenUsage != nil {
		accumulateAgenticUsage(&s.result.Usage, chunk.ResponseMeta.TokenUsage)
	}
	for _, block := range chunk.ContentBlocks {
		if block == nil {
			return errors.New("agentic stream contains a nil block")
		}
		index := s.nextUnindexed
		if block.StreamingMeta != nil {
			if s.mode == "unindexed" {
				return errors.New("agentic stream mixes indexed and unindexed blocks")
			}
			s.mode = "indexed"
			index = block.StreamingMeta.Index
			if index < 0 {
				return errors.New("agentic stream block index is negative")
			}
		} else {
			if s.mode == "indexed" {
				return errors.New("agentic stream mixes indexed and unindexed blocks")
			}
			s.mode = "unindexed"
			s.nextUnindexed++
		}
		ledger, ok := s.indexed[index]
		if !ok {
			if len(s.indexed) >= s.config.limits.MaxBlocks {
				return errors.New("agentic stream block limit exceeded")
			}
			context, err := s.resolver.ResolveBlock(index, block.Type)
			if err != nil {
				return fmt.Errorf("resolve agentic block %d: %w", index, err)
			}
			ledger = blockLedger{kind: block.Type, context: context}
			s.indexed[index] = ledger
		} else if ledger.kind != block.Type {
			return fmt.Errorf("agentic stream block %d changed kind from %s to %s", index, ledger.kind, block.Type)
		}
		normalized, updated, err := normalizeStreamBlock(block, ledger)
		if err != nil {
			return fmt.Errorf("agentic stream block %d: %w", index, err)
		}
		s.indexed[index] = updated
		if err := s.emitTransient(chunk.Role, normalized, ledger.context); err != nil {
			return err
		}
	}
	return nil
}

func normalizeStreamBlock(block *schema.ContentBlock, ledger blockLedger) (*schema.ContentBlock, blockLedger, error) {
	copyBlock := *block
	if block.Type != schema.ContentBlockTypeFunctionToolCall || block.FunctionToolCall == nil {
		return &copyBlock, ledger, nil
	}
	call := *block.FunctionToolCall
	if call.CallID != "" {
		if ledger.callID != "" && ledger.callID != call.CallID {
			return nil, ledger, errors.New("function call ID changed")
		}
		ledger.callID = call.CallID
	}
	if call.Name != "" {
		if ledger.name != "" && ledger.name != call.Name {
			return nil, ledger, errors.New("function call name changed")
		}
		ledger.name = call.Name
	}
	call.CallID, call.Name = ledger.callID, ledger.name
	if call.CallID == "" {
		return nil, ledger, errors.New("function call ID is required before argument deltas")
	}
	copyBlock.FunctionToolCall = &call
	return &copyBlock, ledger, nil
}

func (s *agenticDrain) emitTransient(role schema.AgenticRoleType, block *schema.ContentBlock, context convert.AgenticBlockContext) error {
	if s.config.sink == nil || s.detached {
		return validateChunkBlock(role, block, context, s.base, s.config.limits)
	}
	public, err := projectChunkBlock(role, block, context, s.base, s.config.limits)
	if err != nil {
		return err
	}
	transient, err := convert.TransientEventForBlock(public)
	if err != nil {
		// Rich/custom blocks are buffered until commit, but their unions and limits
		// were still validated by projectChunkBlock.
		if errors.Is(err, convert.ErrNoTransientNativeEvent) {
			return nil
		}
		return err
	}
	if err := s.config.sink.Emit(transient.Event); err != nil {
		s.result.ObserverErr = err
		s.config.sink.Detach(err)
		s.detached = true
		return nil
	}
	s.result.DeliveredTransient = append(s.result.DeliveredTransient, transient.Event)
	return nil
}

func validateChunkBlock(role schema.AgenticRoleType, block *schema.ContentBlock, context convert.AgenticBlockContext, base convert.AgenticIdentityV1, limits convert.ProjectionLimits) error {
	_, err := projectChunkBlock(role, block, context, base, limits)
	return err
}
func projectChunkBlock(role schema.AgenticRoleType, block *schema.ContentBlock, context convert.AgenticBlockContext, base convert.AgenticIdentityV1, limits convert.ProjectionLimits) (convert.PublicContentBlock, error) {
	copyBlock := *block
	copyBlock.StreamingMeta = nil
	projection, err := convert.ProjectAgenticMessage(&schema.AgenticMessage{Role: role, ContentBlocks: []*schema.ContentBlock{&copyBlock}}, convert.AgenticProjectionContext{Identity: base, Blocks: []convert.AgenticBlockContext{context}, Limits: limits})
	if err != nil {
		return convert.PublicContentBlock{}, err
	}
	return projection.ContentBlocks[0], nil
}

func (s *agenticDrain) finish(primary error) (*AgenticResult, error) {
	if len(s.chunks) == 0 {
		s.result.Partial = true
		s.result.Terminal = AgenticTerminalError
		if primary == nil {
			primary = errors.New("empty agentic model stream")
		}
		return s.result, primary
	}
	assistant, concatErr := schema.ConcatAgenticMessages(s.chunks)
	if concatErr != nil && primary == nil {
		primary = concatErr
	}
	if concatErr == nil {
		s.result.Assistant = assistant
		contexts, indices := s.finalContexts()
		s.result.SeenBlockIndices = indices
		if primary == nil {
			projection, err := convert.ToAgenticProjection(assistant, convert.AgenticProjectionContext{Identity: s.base, Blocks: contexts, Limits: s.config.limits})
			if err != nil {
				primary = err
			} else {
				s.result.PublicProjection = projection
			}
		}
	}
	if primary != nil {
		s.result.Partial = true
		if errors.Is(primary, context.Canceled) || errors.Is(primary, context.DeadlineExceeded) {
			s.result.Terminal = AgenticTerminalCancelled
		} else {
			s.result.Terminal = AgenticTerminalError
		}
		return s.result, primary
	}
	s.result.Terminal = AgenticTerminalEOF
	return s.result, nil
}

func (s *agenticDrain) finalContexts() ([]convert.AgenticBlockContext, []int) {
	indices := make([]int, 0, len(s.indexed))
	for index := range s.indexed {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	contexts := make([]convert.AgenticBlockContext, 0, len(indices))
	for _, index := range indices {
		contexts = append(contexts, s.indexed[index].context)
	}
	return contexts, indices
}

func streamIdentity(ids AgenticStreamIdentity) convert.AgenticIdentityV1 {
	return convert.AgenticIdentityV1{SessionID: ids.SessionID, ThreadID: ids.SessionID, RunID: ids.RunID, TurnID: ids.TurnID, MessageID: ids.MessageID, AttemptID: ids.AttemptID, AgentPath: append([]convert.AgentPathSegment(nil), ids.AgentPath...)}
}

func validateAgenticStreamIdentity(ids AgenticStreamIdentity, limits convert.ProjectionLimits) error {
	_, err := convert.ProjectAgenticMessage(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}, convert.AgenticProjectionContext{Identity: streamIdentity(ids), Limits: limits})
	return err
}

func accumulateAgenticUsage(target **schema.TokenUsage, next *schema.TokenUsage) {
	if next == nil {
		return
	}
	if *target == nil {
		copy := *next
		*target = &copy
		return
	}
	current := *target
	current.PromptTokens = max(current.PromptTokens, next.PromptTokens)
	current.CompletionTokens = max(current.CompletionTokens, next.CompletionTokens)
	current.TotalTokens = max(current.TotalTokens, next.TotalTokens)
	current.PromptTokenDetails.CachedTokens = max(current.PromptTokenDetails.CachedTokens, next.PromptTokenDetails.CachedTokens)
	current.CompletionTokensDetails.ReasoningTokens = max(current.CompletionTokensDetails.ReasoningTokens, next.CompletionTokensDetails.ReasoningTokens)
}
