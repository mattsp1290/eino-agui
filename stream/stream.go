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
	// Assistant is the concatenated provider response. It is nil when no
	// response can be concatenated, including an empty stream.
	Assistant *schema.Message
	// WireMessages contains completed AG-UI messages whose frames were written
	// successfully, plus the stable owner for correlated tool calls.
	WireMessages []aguitypes.Message
	// ToolOwnerID identifies the WireMessages entry that owns correlated tool
	// calls. It is empty when the stream contains no correlatable tool calls.
	ToolOwnerID string
	// Usage is the per-field maximum of cumulative usage observations received
	// from the provider. It is retained on partial results.
	Usage *schema.TokenUsage
	// Partial reports that the stream opened but did not complete successfully.
	Partial bool
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

	closeReasoning := func() bool {
		if !reasoningOpen {
			return true
		}
		id, content := reasoningID, reasoningContent
		reasoningID, reasoningContent, reasoningOpen = "", "", false
		emit.ReasoningMessageEnd(id)
		emit.ReasoningEnd(id)
		if emit.Err() != nil {
			return false
		}
		result.WireMessages = append(result.WireMessages, aguitypes.Message{ID: id, Role: aguitypes.RoleReasoning, Content: content})
		return true
	}
	closeText := func() bool {
		if !textOpen {
			return true
		}
		id, content := textID, textContent
		textID, textContent, textOpen = "", "", false
		emit.TextEnd(id)
		if emit.Err() != nil {
			return false
		}
		result.WireMessages = append(result.WireMessages, aguitypes.Message{ID: id, Role: aguitypes.RoleAssistant, Content: content})
		return true
	}
	ensureToolOwner := func() bool {
		if result.ToolOwnerID != "" {
			return true
		}
		if !closeReasoning() || !closeText() {
			return false
		}
		result.ToolOwnerID = aguievents.GenerateMessageID()
		toolOwnerIndex = len(result.WireMessages)
		result.WireMessages = append(result.WireMessages, aguitypes.Message{ID: result.ToolOwnerID, Role: aguitypes.RoleAssistant, Content: ""})
		return true
	}
	closeBlocks := func() {
		closeReasoning()
		closeText()
		endToolCalls()
	}
	finish := func(primary error, correlationFailed bool) (*Result, error) {
		closeBlocks()
		if primary == nil && emit.Err() != nil {
			primary = emit.Err()
		}
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
			} else {
				result.Assistant.ToolCalls = mergedToolCalls(toolOrder)
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
			if emit.Err() != nil || !closeText() {
				return finish(emit.Err(), false)
			}
			if !reasoningOpen {
				reasoningID = aguievents.GenerateMessageID()
				emit.ReasoningStart(reasoningID)
				emit.ReasoningMessageStart(reasoningID)
				if emit.Err() != nil {
					return finish(emit.Err(), false)
				}
				reasoningOpen = true
			}
			emit.ReasoningContent(reasoningID, chunk.ReasoningContent)
			if emit.Err() != nil {
				return finish(emit.Err(), false)
			}
			reasoningContent += chunk.ReasoningContent
		}
		if chunk.Content != "" {
			endToolCalls()
			if emit.Err() != nil || !closeReasoning() {
				return finish(emit.Err(), false)
			}
			if !textOpen {
				textID = aguievents.GenerateMessageID()
				emit.TextStart(textID)
				if emit.Err() != nil {
					return finish(emit.Err(), false)
				}
				textOpen = true
			}
			emit.TextContent(textID, chunk.Content)
			if emit.Err() != nil {
				return finish(emit.Err(), false)
			}
			textContent += chunk.Content
		}
		if len(chunk.ToolCalls) > 0 {
			for _, call := range chunk.ToolCalls {
				if call.Index == nil && call.ID == "" {
					continue
				}
				if !ensureToolOwner() {
					return finish(emit.Err(), false)
				}
				buffer, corrErr := correlateToolCall(call, indexed, byID, emit, result.ToolOwnerID, cfg.liveToolCalls, &toolOrder)
				if corrErr != nil {
					return finish(corrErr, true)
				}
				if buffer != nil {
					buffer.update(call)
					if emit.Err() != nil {
						return finish(emit.Err(), false)
					}
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
	typeName        string
	extra           map[string]any
	arguments       string
	pendingArgs     []string
	live            bool
	started         bool
	ended           bool
}

func (b *toolCallBuffer) update(call schema.ToolCall) {
	if b == nil || b.ended {
		return
	}
	if call.ID != "" {
		b.id = call.ID
	}
	if call.Type != "" {
		b.typeName = call.Type
	}
	if call.Function.Name != "" {
		b.name = call.Function.Name
	}
	if call.Extra != nil {
		if b.extra == nil {
			b.extra = make(map[string]any)
		}
		for key, value := range call.Extra {
			b.extra[key] = value
		}
	}
	b.arguments += call.Function.Arguments
	if !b.live || b.emit == nil {
		return
	}
	if b.started {
		b.emit.ToolArgs(b.id, call.Function.Arguments)
		return
	}
	if call.Function.Arguments != "" {
		b.pendingArgs = append(b.pendingArgs, call.Function.Arguments)
	}
	b.startIfReady()
}

func mergedToolCalls(buffers []*toolCallBuffer) []schema.ToolCall {
	if len(buffers) == 0 {
		return nil
	}
	out := make([]schema.ToolCall, 0, len(buffers))
	for _, buffer := range buffers {
		if buffer == nil {
			continue
		}
		call := schema.ToolCall{
			Index: buffer.index,
			ID:    buffer.id,
			Type:  buffer.typeName,
			Function: schema.FunctionCall{
				Name:      buffer.name,
				Arguments: buffer.arguments,
			},
			Extra: buffer.extra,
		}
		out = append(out, call)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
