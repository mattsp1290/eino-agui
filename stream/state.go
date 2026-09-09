package stream

import (
	"errors"
	"fmt"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/emitter"
)

type turnState struct {
	emit          *emitter.Emitter
	liveToolCalls bool
	result        *Result
	chunks        []*schema.Message

	textID      string
	textContent string
	textOpen    bool

	reasoningID      string
	reasoningContent string
	reasoningOpen    bool

	toolOwnerIndex int
	indexed        map[int]*toolCallBuffer
	byID           map[string]*toolCallBuffer
	toolOrder      []*toolCallBuffer
}

func newTurnState(emit *emitter.Emitter, liveToolCalls bool) *turnState {
	return &turnState{
		emit:           emit,
		liveToolCalls:  liveToolCalls,
		result:         &Result{},
		toolOwnerIndex: -1,
		indexed:        make(map[int]*toolCallBuffer),
		byID:           make(map[string]*toolCallBuffer),
	}
}

func (s *turnState) applyChunk(chunk *schema.Message) error {
	s.chunks = append(s.chunks, chunk)
	if chunk == nil {
		return nil
	}
	accumulateUsage(&s.result.Usage, chunk.ResponseMeta)
	if chunk.ReasoningContent != "" {
		if err := s.applyReasoning(chunk.ReasoningContent); err != nil {
			return err
		}
	}
	if chunk.Content != "" {
		if err := s.applyText(chunk.Content); err != nil {
			return err
		}
	}
	return s.applyToolCalls(chunk.ToolCalls)
}

func (s *turnState) applyReasoning(delta string) error {
	if err := s.endToolCalls(); err != nil {
		return err
	}
	if err := s.closeText(); err != nil {
		return err
	}
	if !s.reasoningOpen {
		s.reasoningID = aguievents.GenerateMessageID()
		s.emit.ReasoningStart(s.reasoningID)
		s.emit.ReasoningMessageStart(s.reasoningID)
		if err := s.emit.Err(); err != nil {
			return err
		}
		s.reasoningOpen = true
	}
	s.emit.ReasoningContent(s.reasoningID, delta)
	if err := s.emit.Err(); err != nil {
		return err
	}
	s.reasoningContent += delta
	return nil
}

func (s *turnState) applyText(delta string) error {
	if err := s.endToolCalls(); err != nil {
		return err
	}
	if err := s.closeReasoning(); err != nil {
		return err
	}
	if !s.textOpen {
		s.textID = aguievents.GenerateMessageID()
		s.emit.TextStart(s.textID)
		if err := s.emit.Err(); err != nil {
			return err
		}
		s.textOpen = true
	}
	s.emit.TextContent(s.textID, delta)
	if err := s.emit.Err(); err != nil {
		return err
	}
	s.textContent += delta
	return nil
}

func (s *turnState) applyToolCalls(calls []schema.ToolCall) error {
	for _, call := range calls {
		if call.Index == nil && call.ID == "" {
			continue
		}
		if err := s.ensureToolOwner(); err != nil {
			return err
		}
		buffer, err := correlateToolCall(call, s.indexed, s.byID, s.emit, s.result.ToolOwnerID, s.liveToolCalls, &s.toolOrder)
		if err != nil {
			return err
		}
		buffer.update(call)
		if err := s.emit.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *turnState) closeReasoning() error {
	if !s.reasoningOpen {
		return nil
	}
	id, content := s.reasoningID, s.reasoningContent
	s.reasoningID, s.reasoningContent, s.reasoningOpen = "", "", false
	s.emit.ReasoningMessageEnd(id)
	s.emit.ReasoningEnd(id)
	if err := s.emit.Err(); err != nil {
		return err
	}
	s.result.WireMessages = append(s.result.WireMessages, aguitypes.Message{ID: id, Role: aguitypes.RoleReasoning, Content: content})
	return nil
}

func (s *turnState) closeText() error {
	if !s.textOpen {
		return nil
	}
	id, content := s.textID, s.textContent
	s.textID, s.textContent, s.textOpen = "", "", false
	s.emit.TextEnd(id)
	if err := s.emit.Err(); err != nil {
		return err
	}
	s.result.WireMessages = append(s.result.WireMessages, aguitypes.Message{ID: id, Role: aguitypes.RoleAssistant, Content: content})
	return nil
}

func (s *turnState) ensureToolOwner() error {
	if s.result.ToolOwnerID != "" {
		return nil
	}
	if err := s.closeReasoning(); err != nil {
		return err
	}
	if err := s.closeText(); err != nil {
		return err
	}
	s.result.ToolOwnerID = aguievents.GenerateMessageID()
	s.toolOwnerIndex = len(s.result.WireMessages)
	s.result.WireMessages = append(s.result.WireMessages, aguitypes.Message{ID: s.result.ToolOwnerID, Role: aguitypes.RoleAssistant, Content: ""})
	return nil
}

func (s *turnState) endToolCalls() error {
	if s.liveToolCalls {
		for _, call := range s.toolOrder {
			call.end()
		}
	}
	return s.emit.Err()
}

func (s *turnState) closeBlocks() error {
	var first error
	if err := s.closeReasoning(); err != nil {
		first = err
	}
	if err := s.closeText(); err != nil && first == nil {
		first = err
	}
	if err := s.endToolCalls(); err != nil && first == nil {
		first = err
	}
	return first
}

func (s *turnState) finish(primary error) (*Result, error) {
	if err := s.closeBlocks(); primary == nil {
		primary = err
	}
	if len(s.chunks) == 0 {
		s.result.Partial = true
		if primary == nil {
			primary = fmt.Errorf("empty model stream")
		}
		return s.result, primary
	}

	assistant, concatErr := schema.ConcatMessages(s.chunks)
	if concatErr == nil {
		s.result.Assistant = assistant
		var correlationErr *CorrelationError
		if errors.As(primary, &correlationErr) {
			s.result.Assistant.ToolCalls = nil
		} else {
			s.result.Assistant.ToolCalls = mergedToolCalls(s.toolOrder)
		}
	} else if primary == nil {
		primary = concatErr
	}

	var correlationErr *CorrelationError
	if !errors.As(primary, &correlationErr) && s.result.Assistant != nil && s.toolOwnerIndex >= 0 {
		s.result.WireMessages[s.toolOwnerIndex].ToolCalls = convert.ToAGUIToolCalls(s.result.Assistant.ToolCalls)
	}
	s.result.Partial = primary != nil
	return s.result, primary
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
