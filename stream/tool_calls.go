package stream

import (
	"fmt"

	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/emitter"
)

func correlateToolCall(call schema.ToolCall, indexed map[int]*toolCallBuffer, byID map[string]*toolCallBuffer, emit *emitter.Emitter, ownerID string, live bool, order *[]*toolCallBuffer) (*toolCallBuffer, error) {
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
		out = append(out, schema.ToolCall{
			Index: buffer.index,
			ID:    buffer.id,
			Type:  buffer.typeName,
			Function: schema.FunctionCall{
				Name:      buffer.name,
				Arguments: buffer.arguments,
			},
			Extra: buffer.extra,
		})
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
