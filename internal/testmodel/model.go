package testmodel

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var _ model.ToolCallingChatModel = (*Model)(nil)

// Model is a deterministic ToolCallingChatModel for tests.
type Model struct {
	state *state
	tools []*schema.ToolInfo
}

type state struct {
	mu     sync.Mutex
	turns  [][]*schema.Message
	replay bool
	calls  int
}

// NewScriptedModel returns a model that consumes one turn per Generate or
// Stream call.
func NewScriptedModel(turns ...[]*schema.Message) *Model {
	return &Model{state: &state{turns: cloneTurns(turns), replay: false}}
}

// NewReplayModel returns a model that replays the same chunks on every Generate
// or Stream call.
func NewReplayModel(chunks []*schema.Message) *Model {
	return &Model{state: &state{turns: cloneTurns([][]*schema.Message{chunks}), replay: true}}
}

// Generate returns the concatenated messages for the next scripted turn.
func (m *Model) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	chunks, err := m.nextTurn()
	if err != nil {
		return nil, err
	}
	return schema.ConcatMessages(chunks)
}

// Stream returns the next scripted turn as an eino StreamReader.
func (m *Model) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	chunks, err := m.nextTurn()
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray(chunks), nil
}

// WithTools returns a copy of the model with the provided tool definitions.
func (m *Model) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	next := &Model{
		state: m.state,
		tools: append([]*schema.ToolInfo(nil), tools...),
	}
	return next, nil
}

// Calls returns the number of Generate or Stream calls made against the model.
func (m *Model) Calls() int {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.calls
}

// Tools returns the tools bound through WithTools.
func (m *Model) Tools() []*schema.ToolInfo {
	return append([]*schema.ToolInfo(nil), m.tools...)
}

func (m *Model) nextTurn() ([]*schema.Message, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()

	if len(m.state.turns) == 0 {
		return nil, fmt.Errorf("testmodel: no scripted turns remain")
	}

	var turn []*schema.Message
	if m.state.replay {
		turn = m.state.turns[0]
	} else {
		turn = m.state.turns[0]
		m.state.turns = m.state.turns[1:]
	}
	m.state.calls++
	return cloneMessages(turn), nil
}

func cloneTurns(turns [][]*schema.Message) [][]*schema.Message {
	cloned := make([][]*schema.Message, len(turns))
	for i := range turns {
		cloned[i] = cloneMessages(turns[i])
	}
	return cloned
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	cloned := make([]*schema.Message, len(messages))
	indexes := map[int]*int{}
	for i, msg := range messages {
		if msg == nil {
			continue
		}
		next := *msg
		if msg.ToolCalls != nil {
			next.ToolCalls = make([]schema.ToolCall, len(msg.ToolCalls))
			copy(next.ToolCalls, msg.ToolCalls)
			for j := range next.ToolCalls {
				if next.ToolCalls[j].Index == nil {
					continue
				}
				value := *next.ToolCalls[j].Index
				stable := indexes[value]
				if stable == nil {
					v := value
					stable = &v
					indexes[value] = stable
				}
				next.ToolCalls[j].Index = stable
			}
		}
		if msg.AssistantGenMultiContent != nil {
			next.AssistantGenMultiContent = cloneOutputParts(msg.AssistantGenMultiContent)
		}
		if msg.Extra != nil {
			next.Extra = cloneMap(msg.Extra)
		}
		cloned[i] = &next
	}
	return cloned
}

func cloneOutputParts(parts []schema.MessageOutputPart) []schema.MessageOutputPart {
	cloned := make([]schema.MessageOutputPart, len(parts))
	for i, part := range parts {
		next := part
		if part.Reasoning != nil {
			reasoning := *part.Reasoning
			next.Reasoning = &reasoning
		}
		if part.StreamingMeta != nil {
			meta := *part.StreamingMeta
			next.StreamingMeta = &meta
		}
		if part.Extra != nil {
			next.Extra = cloneMap(part.Extra)
		}
		cloned[i] = next
	}
	return cloned
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
