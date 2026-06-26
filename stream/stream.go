package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/mattsp1290/eino-agui/emitter"
)

// Option configures StreamTurn.
type Option func(*config)

type config struct {
	liveToolCalls bool
}

// WithLiveToolCallEvents controls whether streamed model tool calls are emitted
// live as TOOL_CALL_* events. When enabled, callers must not also emit post-turn
// tool proposals for the same calls.
func WithLiveToolCallEvents(enabled bool) Option {
	return func(cfg *config) {
		cfg.liveToolCalls = enabled
	}
}

// StreamTurn streams one model turn, emits AG-UI reasoning/text/tool-call
// events as chunks arrive, and returns the concatenated assistant message.
func StreamTurn(ctx context.Context, emit *emitter.Emitter, cm model.ToolCallingChatModel, messages []*schema.Message, opts ...Option) (*schema.Message, error) {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	sr, err := cm.Stream(ctx, messages)
	if err != nil {
		return nil, err
	}
	defer sr.Close()

	var chunks []*schema.Message
	var textID string
	var reasoningID string
	textOpen, reasoningOpen := false, false
	tcs := map[string]*toolCallBuffer{}
	var tcOrder []string

	closeReasoning := func() {
		if reasoningOpen {
			emit.ReasoningMessageEnd(reasoningID)
			emit.ReasoningEnd(reasoningID)
			reasoningOpen = false
		}
	}
	closeText := func() {
		if textOpen {
			emit.TextEnd(textID)
			textOpen = false
		}
	}
	streamToolCallChunk := func(chunk *schema.Message) {
		if len(chunk.ToolCalls) == 0 {
			return
		}
		closeReasoning()
		closeText()
		for _, tc := range chunk.ToolCalls {
			key := toolCallKey(tc)
			buf := tcs[key]
			if buf == nil {
				buf = &toolCallBuffer{emit: emit}
				tcs[key] = buf
				tcOrder = append(tcOrder, key)
			}
			buf.update(tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
	}
	endStreamedToolCalls := func() {
		closeReasoning()
		closeText()
		for _, key := range tcOrder {
			tcs[key].end()
		}
	}
	closeOpenBlocks := func() {
		if cfg.liveToolCalls {
			endStreamedToolCalls()
			return
		}
		closeReasoning()
		closeText()
	}
	defer closeOpenBlocks()

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := emit.Err(); err != nil {
			return nil, err
		}
		chunk, recvErr := sr.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		if chunk.ReasoningContent != "" {
			if textOpen {
				emit.TextEnd(textID)
				textOpen = false
			}
			if !reasoningOpen {
				reasoningID = aguievents.GenerateMessageID()
				emit.ReasoningStart(reasoningID)
				emit.ReasoningMessageStart(reasoningID)
				reasoningOpen = true
			}
			emit.ReasoningContent(reasoningID, chunk.ReasoningContent)
		}
		if chunk.Content != "" {
			closeReasoning()
			if !textOpen {
				textID = aguievents.GenerateMessageID()
				emit.TextStart(textID)
				textOpen = true
			}
			emit.TextContent(textID, chunk.Content)
		}
		if cfg.liveToolCalls {
			streamToolCallChunk(chunk)
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("empty model stream")
	}
	return schema.ConcatMessages(chunks)
}

func toolCallKey(tc schema.ToolCall) string {
	if tc.Index != nil {
		return "i" + strconv.Itoa(*tc.Index)
	}
	if tc.ID != "" {
		return "d" + tc.ID
	}
	return "p0"
}

type toolCallBuffer struct {
	emit        *emitter.Emitter
	id          string
	name        string
	pendingArgs []string
	started     bool
	ended       bool
}

func (b *toolCallBuffer) update(id, name, argsDelta string) {
	if b == nil || b.emit == nil || b.ended {
		return
	}
	if id != "" {
		b.id = id
	}
	if name != "" {
		b.name = name
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
	if b == nil || b.emit == nil || b.ended {
		return
	}
	b.startIfReady()
	if b.started {
		b.emit.ToolEnd(b.id)
	}
	b.ended = true
}

func (b *toolCallBuffer) startIfReady() {
	if b.started || b.id == "" || b.name == "" {
		return
	}
	b.emit.ToolStart(b.id, b.name)
	b.started = true
	for _, arg := range b.pendingArgs {
		b.emit.ToolArgs(b.id, arg)
	}
	b.pendingArgs = nil
}
