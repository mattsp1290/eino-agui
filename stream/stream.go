package stream

import (
	"context"
	"errors"
	"fmt"
	"io"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/emitter"
)

// Result preserves both the provider-facing Eino response and the exact
// message identities represented by the emitted AG-UI stream.
type Result struct {
	Assistant    *schema.Message
	WireMessages []aguitypes.Message
	ToolOwnerID  string
	Usage        *schema.TokenUsage
	Partial      bool
}

// CorrelationError reports an ambiguous streamed tool-call identity.
type CorrelationError struct{ Message string }

func (e *CorrelationError) Error() string { return "tool-call correlation: " + e.Message }

// Option configures StreamTurn.
type Option func(*config)

type config struct{ liveToolCalls bool }

// WithLiveToolCallEvents controls whether streamed model tool calls are emitted
// live as TOOL_CALL_* events. When enabled, callers must not also emit post-turn
// tool proposals for the same calls.
func WithLiveToolCallEvents(enabled bool) Option {
	return func(cfg *config) { cfg.liveToolCalls = enabled }
}

// StreamTurn streams one classic Eino model turn and returns its provider
// response together with the AG-UI wire transcript and tool-owner identity.
// Once the model stream opens, errors return a non-nil partial Result.
func StreamTurn(ctx context.Context, emit *emitter.Emitter, cm model.ToolCallingChatModel, messages []*schema.Message, opts ...Option) (*Result, error) {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	sr, err := cm.Stream(ctx, messages)
	if err != nil {
		return nil, err
	}
	defer sr.Close()

	result := &Result{}
	var chunks []*schema.Message
	var textID, textContent string
	var reasoningID, reasoningContent string
	textOpen, reasoningOpen := false, false
	toolOwnerIndex := -1
	indexed := make(map[int]*toolCallBuffer)
	byID := make(map[string]*toolCallBuffer)
	var toolOrder []*toolCallBuffer
	endToolCalls := func() {
		if cfg.liveToolCalls {
			for _, call := range toolOrder {
				call.end()
			}
		}
	}

	closeReasoning := func() {
		if !reasoningOpen {
			return
		}
		emit.ReasoningMessageEnd(reasoningID)
		emit.ReasoningEnd(reasoningID)
		result.WireMessages = append(result.WireMessages, aguitypes.Message{ID: reasoningID, Role: aguitypes.RoleReasoning, Content: reasoningContent})
		reasoningID, reasoningContent, reasoningOpen = "", "", false
	}
	closeText := func() {
		if !textOpen {
			return
		}
		emit.TextEnd(textID)
		result.WireMessages = append(result.WireMessages, aguitypes.Message{ID: textID, Role: aguitypes.RoleAssistant, Content: textContent})
		textID, textContent, textOpen = "", "", false
	}
	ensureToolOwner := func() {
		if result.ToolOwnerID != "" {
			return
		}
		closeReasoning()
		closeText()
		result.ToolOwnerID = aguievents.GenerateMessageID()
		toolOwnerIndex = len(result.WireMessages)
		result.WireMessages = append(result.WireMessages, aguitypes.Message{ID: result.ToolOwnerID, Role: aguitypes.RoleAssistant, Content: ""})
	}
	closeBlocks := func() {
		closeReasoning()
		closeText()
		endToolCalls()
	}
	finish := func(primary error, correlationFailed bool) (*Result, error) {
		closeBlocks()
		if len(chunks) == 0 {
			result.Partial = true
			if primary == nil {
				primary = fmt.Errorf("empty model stream")
			}
			return result, primary
		}
		assistant, concatErr := schema.ConcatMessages(chunks)
		if concatErr == nil {
			result.Assistant = assistant
			if correlationFailed {
				result.Assistant.ToolCalls = nil
			}
		} else if primary == nil {
			primary = concatErr
		}
		if !correlationFailed && result.Assistant != nil && toolOwnerIndex >= 0 {
			result.WireMessages[toolOwnerIndex].ToolCalls = convert.ToAGUIToolCalls(result.Assistant.ToolCalls)
		}
		result.Partial = primary != nil
		return result, primary
	}

	for {
		if err := ctx.Err(); err != nil {
			return finish(err, false)
		}
		if err := emit.Err(); err != nil {
			return finish(err, false)
		}
		chunk, recvErr := sr.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return finish(recvErr, false)
		}
		chunks = append(chunks, chunk)
		if chunk == nil {
			continue
		}
		accumulateUsage(&result.Usage, chunk.ResponseMeta)

		if chunk.ReasoningContent != "" {
			endToolCalls()
			closeText()
			if !reasoningOpen {
				reasoningID = aguievents.GenerateMessageID()
				emit.ReasoningStart(reasoningID)
				emit.ReasoningMessageStart(reasoningID)
				reasoningOpen = true
			}
			reasoningContent += chunk.ReasoningContent
			emit.ReasoningContent(reasoningID, chunk.ReasoningContent)
		}
		if chunk.Content != "" {
			endToolCalls()
			closeReasoning()
			if !textOpen {
				textID = aguievents.GenerateMessageID()
				emit.TextStart(textID)
				textOpen = true
			}
			textContent += chunk.Content
			emit.TextContent(textID, chunk.Content)
		}
		if len(chunk.ToolCalls) > 0 {
			ensureToolOwner()
			for _, call := range chunk.ToolCalls {
				buffer, corrErr := correlateToolCall(call, indexed, byID, emit, result.ToolOwnerID, cfg.liveToolCalls, &toolOrder)
				if corrErr != nil {
					return finish(corrErr, true)
				}
				if buffer != nil {
					buffer.update(call.ID, call.Function.Name, call.Function.Arguments)
				}
			}
		}
	}
	if err := emit.Err(); err != nil {
		return finish(err, false)
	}
	return finish(nil, false)
}

func correlateToolCall(call schema.ToolCall, indexed map[int]*toolCallBuffer, byID map[string]*toolCallBuffer, emit *emitter.Emitter, ownerID string, live bool, order *[]*toolCallBuffer) (*toolCallBuffer, error) {
	if call.Index == nil && call.ID == "" {
		return nil, nil
	}
	if call.Index != nil {
		index := *call.Index
		buffer := indexed[index]
		if buffer == nil {
			buffer = &toolCallBuffer{emit: emit, parentMessageID: ownerID, live: live, index: &index}
			indexed[index] = buffer
			*order = append(*order, buffer)
		}
		if call.ID != "" && buffer.id != "" && buffer.id != call.ID {
			return nil, &CorrelationError{Message: fmt.Sprintf("index %d changed ID from %q to %q", index, buffer.id, call.ID)}
		}
		if call.ID != "" {
			if claimed := byID[call.ID]; claimed != nil && claimed != buffer {
				return nil, &CorrelationError{Message: fmt.Sprintf("ID %q is claimed by multiple stream entries", call.ID)}
			}
			byID[call.ID] = buffer
		}
		return buffer, nil
	}

	buffer := byID[call.ID]
	if buffer == nil {
		buffer = &toolCallBuffer{emit: emit, parentMessageID: ownerID, live: live}
		byID[call.ID] = buffer
		*order = append(*order, buffer)
	}
	return buffer, nil
}

func accumulateUsage(target **schema.TokenUsage, meta *schema.ResponseMeta) {
	if meta == nil || meta.Usage == nil {
		return
	}
	if *target == nil {
		*target = &schema.TokenUsage{}
	}
	usage := *target
	usage.PromptTokens = max(usage.PromptTokens, meta.Usage.PromptTokens)
	usage.CompletionTokens = max(usage.CompletionTokens, meta.Usage.CompletionTokens)
	usage.TotalTokens = max(usage.TotalTokens, meta.Usage.TotalTokens)
	usage.PromptTokenDetails.CachedTokens = max(usage.PromptTokenDetails.CachedTokens, meta.Usage.PromptTokenDetails.CachedTokens)
	usage.CompletionTokensDetails.ReasoningTokens = max(usage.CompletionTokensDetails.ReasoningTokens, meta.Usage.CompletionTokensDetails.ReasoningTokens)
}

type toolCallBuffer struct {
	emit            *emitter.Emitter
	parentMessageID string
	index           *int
	id              string
	name            string
	pendingArgs     []string
	live            bool
	started         bool
	ended           bool
}

func (b *toolCallBuffer) update(id, name, argsDelta string) {
	if b == nil || b.ended {
		return
	}
	if id != "" {
		b.id = id
	}
	if name != "" {
		b.name = name
	}
	if !b.live || b.emit == nil {
		return
	}
	if b.started {
		b.emit.ToolArgs(b.id, argsDelta)
		return
	}
	if argsDelta != "" {
		b.pendingArgs = append(b.pendingArgs, argsDelta)
	}
	b.startIfReady()
}

func (b *toolCallBuffer) end() {
	if b == nil || b.ended {
		return
	}
	b.startIfReady()
	if b.started {
		b.emit.ToolEnd(b.id)
	}
	b.ended = true
}

func (b *toolCallBuffer) startIfReady() {
	if !b.live || b.emit == nil || b.started || b.id == "" || b.name == "" {
		return
	}
	b.emit.ToolStart(b.id, b.name, b.parentMessageID)
	b.started = true
	for _, arg := range b.pendingArgs {
		b.emit.ToolArgs(b.id, arg)
	}
	b.pendingArgs = nil
}
