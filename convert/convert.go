package convert

import (
	"log/slog"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
)

// EinoOption configures AG-UI to eino message conversion.
type EinoOption func(*einoConfig)

type einoConfig struct {
	vision bool
}

// WithVisionSupport controls whether image content is forwarded to eino. When
// false, only text fragments are forwarded from multimodal user messages.
func WithVisionSupport(enabled bool) EinoOption {
	return func(cfg *einoConfig) {
		cfg.vision = enabled
	}
}

// ToEinoMessages maps AG-UI request message history into eino messages. Roles
// eino has no use for in model input, such as reasoning and activity, are
// skipped.
func ToEinoMessages(in []types.Message, opts ...EinoOption) []*schema.Message {
	cfg := einoConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	out := make([]*schema.Message, 0, len(in))
	for _, m := range in {
		switch m.Role {
		case types.RoleUser:
			if cfg.vision {
				if msg := ToEinoUserMessage(m); msg != nil {
					out = append(out, msg)
				}
			} else if text := MessageText(m); text != "" {
				out = append(out, schema.UserMessage(text))
			}
		case types.RoleSystem, types.RoleDeveloper:
			if text := MessageText(m); text != "" {
				out = append(out, schema.SystemMessage(text))
			}
		case types.RoleAssistant:
			content, _ := m.ContentString()
			out = append(out, &schema.Message{
				Role:      schema.Assistant,
				Content:   content,
				ToolCalls: ToEinoToolCalls(m.ToolCalls),
			})
		case types.RoleTool:
			content, _ := m.ContentString()
			out = append(out, schema.ToolMessage(content, m.ToolCallID))
		}
	}
	return out
}

// MessageText returns the message's usable text. For multimodal content, text
// fragments are joined with newlines and non-text fragments are dropped.
func MessageText(m types.Message) string {
	if s, ok := m.ContentString(); ok {
		return s
	}
	parts, ok := m.ContentInputContents()
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == types.InputContentTypeText && p.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// ToEinoUserMessage converts an AG-UI user message to an eino message,
// preserving image parts for callers that have enabled vision support.
// Non-image, non-text parts are dropped. Nil is returned when no usable content
// remains after filtering.
func ToEinoUserMessage(m types.Message) *schema.Message {
	parts, hasParts := m.ContentInputContents()
	if !hasParts {
		text, _ := m.ContentString()
		if text == "" {
			return nil
		}
		return schema.UserMessage(text)
	}

	var textBuf strings.Builder
	var multiParts []schema.MessageInputPart
	hasNonText := false

	for _, p := range parts {
		switch p.Type {
		case types.InputContentTypeText:
			if p.Text != "" {
				if textBuf.Len() > 0 {
					textBuf.WriteByte('\n')
				}
				textBuf.WriteString(p.Text)
				multiParts = append(multiParts, schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeText,
					Text: p.Text,
				})
			}
		case types.InputContentTypeImage:
			if part, ok := ToEinoImagePart(p); ok {
				multiParts = append(multiParts, part)
				hasNonText = true
			}
		default:
			slog.Warn("unsupported multimodal content type, dropping", "type", p.Type)
		}
	}

	if len(multiParts) == 0 {
		return nil
	}
	if !hasNonText {
		return schema.UserMessage(textBuf.String())
	}
	return &schema.Message{
		Role:                  schema.User,
		UserInputMultiContent: multiParts,
	}
}

// ToEinoImagePart maps an AG-UI image fragment to an eino MessageInputPart.
func ToEinoImagePart(p types.InputContent) (schema.MessageInputPart, bool) {
	img := &schema.MessageInputImage{}
	if p.Source != nil {
		switch p.Source.Type {
		case types.InputContentSourceTypeURL:
			img.URL = &p.Source.Value
		case types.InputContentSourceTypeData:
			img.Base64Data = &p.Source.Value
			img.MIMEType = p.Source.MimeType
		default:
			slog.Warn("unknown image source type, dropping", "source_type", p.Source.Type)
			return schema.MessageInputPart{}, false
		}
	} else if p.URL != "" {
		img.URL = &p.URL
	} else if p.Data != "" {
		img.Base64Data = &p.Data
		img.MIMEType = p.MimeType
	} else {
		slog.Warn("image part has no source URL or data, dropping")
		return schema.MessageInputPart{}, false
	}
	return schema.MessageInputPart{
		Type:  schema.ChatMessagePartTypeImageURL,
		Image: img,
	}, true
}

// ToEinoToolCalls maps AG-UI tool calls to eino tool calls.
func ToEinoToolCalls(tcs []types.ToolCall) []schema.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]schema.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		out = append(out, schema.ToolCall{
			ID:       tc.ID,
			Type:     "function",
			Function: schema.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
		})
	}
	return out
}

// ToAGUIMessages converts an eino conversation into AG-UI messages for a
// MESSAGES_SNAPSHOT event, assigning fresh IDs and conforming to the SDK's
// per-role validation rules.
func ToAGUIMessages(msgs []*schema.Message) []types.Message {
	out := make([]types.Message, 0, len(msgs))
	for _, m := range msgs {
		am := types.Message{ID: events.GenerateMessageID(), Content: m.Content}
		switch m.Role {
		case schema.System:
			am.Role = types.RoleSystem
		case schema.User:
			am.Role = types.RoleUser
		case schema.Assistant:
			am.Role = types.RoleAssistant
			am.ToolCalls = ToAGUIToolCalls(m.ToolCalls)
		case schema.Tool:
			am.Role = types.RoleTool
			am.ToolCallID = m.ToolCallID
		default:
			continue
		}
		out = append(out, am)
	}
	return out
}

// ToAGUIToolCalls maps eino tool calls to AG-UI tool calls.
func ToAGUIToolCalls(tcs []schema.ToolCall) []types.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]types.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		out = append(out, types.ToolCall{
			ID:       tc.ID,
			Type:     types.ToolCallTypeFunction,
			Function: types.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
		})
	}
	return out
}
