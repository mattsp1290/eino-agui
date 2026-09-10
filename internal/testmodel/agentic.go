package testmodel

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var _ model.AgenticModel = (*AgenticModel)(nil)

// AgenticModel is a deterministic implementation of Eino's real agentic
// model interface. Each Stream call returns a fresh reader over the fixtures.
type AgenticModel struct {
	mu        sync.Mutex
	chunks    []*schema.AgenticMessage
	openErr   error
	calls     int
	lastInput []*schema.AgenticMessage
}

func NewAgenticReplayModel(chunks []*schema.AgenticMessage) *AgenticModel {
	return &AgenticModel{chunks: append([]*schema.AgenticMessage(nil), chunks...)}
}

func NewAgenticOpenErrorModel(err error) *AgenticModel { return &AgenticModel{openErr: err} }

func (m *AgenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, options ...model.Option) (*schema.AgenticMessage, error) {
	reader, err := m.Stream(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return schema.ConcatAgenticMessages(m.chunks)
}

func (m *AgenticModel) Stream(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastInput = append([]*schema.AgenticMessage(nil), input...)
	if m.openErr != nil {
		return nil, m.openErr
	}
	if m.chunks == nil {
		return schema.StreamReaderFromArray([]*schema.AgenticMessage{}), nil
	}
	return schema.StreamReaderFromArray(append([]*schema.AgenticMessage(nil), m.chunks...)), nil
}

func (m *AgenticModel) Calls() int { m.mu.Lock(); defer m.mu.Unlock(); return m.calls }

func AgenticTextChunks(index int, fragments ...string) []*schema.AgenticMessage {
	out := make([]*schema.AgenticMessage, len(fragments))
	for i, fragment := range fragments {
		out[i] = &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.AssistantGenText{Text: fragment}, &schema.StreamingMeta{Index: index})}}
	}
	return out
}

func AgenticToolCallChunks(index int, id, name string, fragments ...string) []*schema.AgenticMessage {
	if len(fragments) == 0 {
		fragments = []string{""}
	}
	out := make([]*schema.AgenticMessage, len(fragments))
	for i, fragment := range fragments {
		chunkID, chunkName := "", ""
		if i == 0 {
			chunkID, chunkName = id, name
		}
		out[i] = &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.FunctionToolCall{CallID: chunkID, Name: chunkName, Arguments: fragment}, &schema.StreamingMeta{Index: index})}}
	}
	return out
}

func (m *AgenticModel) String() string { return fmt.Sprintf("AgenticModel(calls=%d)", m.Calls()) }
