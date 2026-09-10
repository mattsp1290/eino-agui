package convert

import (
	"errors"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
)

var ErrNoTransientNativeEvent = errors.New("content kind has no transient native event")

type ProjectedAgenticBlock struct {
	Public     PublicContentBlock `json:"public"`
	Native     []events.Event     `json:"-"`
	Supplement AgenticEnvelopeV1  `json:"supplement"`
}

type AgenticProjection struct {
	Public        *PublicAgenticMessage   `json:"public"`
	NativeMessage *types.Message          `json:"-"`
	Blocks        []ProjectedAgenticBlock `json:"blocks"`
	Digest        CandidateDigestV1       `json:"digest"`
}

type TransientBlock struct {
	Identity AgenticIdentityV1
	Event    events.Event
}

func ToAgenticProjection(msg *schema.AgenticMessage, ctx AgenticProjectionContext) (*AgenticProjection, error) {
	public, err := ProjectAgenticMessage(msg, ctx)
	if err != nil {
		return nil, err
	}
	out := &AgenticProjection{Public: public, Digest: public.Digest, Blocks: make([]ProjectedAgenticBlock, len(public.ContentBlocks))}
	for i := range public.ContentBlocks {
		block := public.ContentBlocks[i]
		native, err := nativeEventsForBlock(block, false)
		if err != nil {
			return nil, &ProjectionError{Block: i, Path: "native", Err: err}
		}
		envelope := AgenticEnvelopeV1{Version: AgenticSchemaVersion, Kind: EnvelopeContentBlock, Identity: cloneIdentity(block.Identity), ContentBlock: &block}
		digest, err := LifecycleDigestV1(&envelope)
		if err != nil {
			return nil, err
		}
		envelope.Digest = digest
		out.Blocks[i] = ProjectedAgenticBlock{Public: block, Native: native, Supplement: envelope}
	}
	if public.Role == schema.AgenticRoleTypeUser {
		out.NativeMessage = nativeUserMessage(public)
	}
	return out, nil
}

// nativeUserMessage returns the representable AG-UI user-message view. It is
// deliberately not emitted as a one-message MESSAGES_SNAPSHOT: hosts assemble
// snapshots from their complete committed transcript, so this bridge must not
// overwrite unrelated history while projecting one durable record.
func nativeUserMessage(message *PublicAgenticMessage) *types.Message {
	content := make([]types.InputContent, 0, len(message.ContentBlocks))
	for i := range message.ContentBlocks {
		block := message.ContentBlocks[i]
		switch block.Type {
		case schema.ContentBlockTypeUserInputText:
			content = append(content, types.InputContent{Type: types.InputContentTypeText, Text: derefString(block.Text)})
		case schema.ContentBlockTypeUserInputImage:
			content = append(content, nativeInputMedia(types.InputContentTypeImage, block.Media))
		case schema.ContentBlockTypeUserInputAudio:
			content = append(content, nativeInputMedia(types.InputContentTypeAudio, block.Media))
		case schema.ContentBlockTypeUserInputVideo:
			content = append(content, nativeInputMedia(types.InputContentTypeVideo, block.Media))
		case schema.ContentBlockTypeUserInputFile:
			part := nativeInputMedia(types.InputContentTypeDocument, block.Media)
			if block.Media != nil && block.Media.Name != "" {
				part.Metadata = map[string]any{"filename": block.Media.Name}
			}
			content = append(content, part)
		}
	}
	if len(content) == 0 {
		return nil
	}
	return &types.Message{
		ID:       message.Identity.MessageID,
		Role:     types.RoleUser,
		Content:  content,
		Metadata: types.Metadata{AgenticCustomEventName: cloneIdentity(message.Identity)},
	}
}

func nativeInputMedia(kind string, media *PublicMedia) types.InputContent {
	part := types.InputContent{Type: kind}
	if media == nil {
		return part
	}
	if media.URL != "" {
		part.Source = &types.InputContentSource{Type: types.InputContentSourceTypeURL, Value: media.URL, MimeType: media.MIMEType}
	} else {
		part.Source = &types.InputContentSource{Type: types.InputContentSourceTypeData, Value: media.Base64Data, MimeType: media.MIMEType}
	}
	return part
}

func TransientEventForBlock(block PublicContentBlock) (TransientBlock, error) {
	eventsForBlock, err := nativeEventsForBlock(block, true)
	if err != nil {
		return TransientBlock{}, err
	}
	if len(eventsForBlock) != 1 {
		return TransientBlock{}, errors.New("block does not have one self-contained transient event")
	}
	return TransientBlock{Identity: transientIdentity(block.Identity), Event: eventsForBlock[0]}, nil
}

// CommittedNativeEvents returns a fresh SDK-native representation derived from
// the receipt-bound public block. Emitters use this instead of trusting the
// mutable event cache carried by AgenticProjection.
func CommittedNativeEvents(block PublicContentBlock) ([]events.Event, error) {
	return nativeEventsForBlock(block, false)
}

func nativeEventsForBlock(block PublicContentBlock, transient bool) ([]events.Event, error) {
	id := block.Identity
	if transient {
		id = transientIdentity(id)
	}
	metadata := types.Metadata{AgenticCustomEventName: id}
	attach := func(event events.Event) events.Event { event.GetBaseEvent().Metadata = metadata; return event }
	if transient {
		switch block.Type {
		case schema.ContentBlockTypeReasoning:
			return []events.Event{attach(events.NewReasoningMessageChunkEvent(&id.MessageID, block.Text))}, nil
		case schema.ContentBlockTypeAssistantGenText:
			role := "assistant"
			return []events.Event{attach(events.NewTextMessageChunkEvent(&id.MessageID, &role, block.Text))}, nil
		case schema.ContentBlockTypeFunctionToolCall:
			v := block.FunctionToolCall
			if v == nil {
				return nil, errors.New("function call payload is required")
			}
			event := events.NewToolCallChunkEvent().WithToolCallChunkID(v.CallID).WithToolCallChunkName(v.Name).WithToolCallChunkDelta(v.Arguments).WithToolCallChunkParentMessageID(id.MessageID)
			return []events.Event{attach(event)}, nil
		default:
			return nil, ErrNoTransientNativeEvent
		}
	}

	switch block.Type {
	case schema.ContentBlockTypeReasoning:
		return []events.Event{
			attach(events.NewReasoningStartEvent(id.MessageID)),
			attach(events.NewReasoningMessageStartEvent(id.MessageID, "reasoning")),
			attach(events.NewReasoningMessageContentEvent(id.MessageID, derefString(block.Text))),
			attach(events.NewReasoningMessageEndEvent(id.MessageID)),
			attach(events.NewReasoningEndEvent(id.MessageID)),
		}, nil
	case schema.ContentBlockTypeAssistantGenText:
		return []events.Event{
			attach(events.NewTextMessageStartEvent(id.MessageID, events.WithRole("assistant"))),
			attach(events.NewTextMessageContentEvent(id.MessageID, derefString(block.Text))),
			attach(events.NewTextMessageEndEvent(id.MessageID)),
		}, nil
	case schema.ContentBlockTypeFunctionToolCall:
		v := block.FunctionToolCall
		if v == nil {
			return nil, errors.New("function call payload is required")
		}
		return []events.Event{
			attach(events.NewToolCallStartEvent(v.CallID, v.Name, events.WithParentMessageID(id.MessageID))),
			attach(events.NewToolCallArgsEvent(v.CallID, v.Arguments)),
			attach(events.NewToolCallEndEvent(v.CallID)),
		}, nil
	case schema.ContentBlockTypeFunctionToolResult:
		v := block.FunctionToolResult
		if v != nil && len(v.Content) == 1 && v.Content[0].Type == schema.FunctionToolResultContentBlockTypeText {
			return []events.Event{attach(events.NewToolCallResultEvent(id.MessageID, v.CallID, emptyResult(v.Content[0].Text)))}, nil
		}
	}
	return nil, nil
}

func transientIdentity(id AgenticIdentityV1) AgenticIdentityV1 {
	id = cloneIdentity(id)
	id.Transient = true
	return id
}
func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func emptyResult(v string) string {
	if v == "" {
		return "(empty)"
	}
	return v
}
