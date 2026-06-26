package convert

import (
	"reflect"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
	"github.com/mattsp1290/eino-agui/internal/testids"
)

func TestToEinoMessagesVisionGating(t *testing.T) {
	message := types.Message{
		Role: types.RoleUser,
		Content: []types.InputContent{
			{Type: types.InputContentTypeText, Text: "What is in this image?"},
			{
				Type: types.InputContentTypeImage,
				Source: &types.InputContentSource{
					Type:  types.InputContentSourceTypeURL,
					Value: "https://example.test/cat.png",
				},
			},
			{Type: types.InputContentTypeText, Text: "Second line"},
		},
	}

	vision := ToEinoMessages([]types.Message{message}, WithVisionSupport(true))
	if len(vision) != 1 {
		t.Fatalf("vision messages len = %d, want 1", len(vision))
	}
	if vision[0].Role != schema.User {
		t.Fatalf("vision role = %s, want user", vision[0].Role)
	}
	if vision[0].Content != "" {
		t.Fatalf("vision content = %q, want empty for multimodal", vision[0].Content)
	}
	if len(vision[0].UserInputMultiContent) != 3 {
		t.Fatalf("vision multi content len = %d, want 3", len(vision[0].UserInputMultiContent))
	}
	if got := vision[0].UserInputMultiContent[1].Image.URL; got == nil || *got != "https://example.test/cat.png" {
		t.Fatalf("vision image URL = %v, want cat URL", got)
	}

	textOnly := ToEinoMessages([]types.Message{message})
	if len(textOnly) != 1 {
		t.Fatalf("text-only messages len = %d, want 1", len(textOnly))
	}
	if got, want := textOnly[0].Content, "What is in this image?\nSecond line"; got != want {
		t.Fatalf("text-only content = %q, want %q", got, want)
	}
	if len(textOnly[0].UserInputMultiContent) != 0 {
		t.Fatalf("text-only multi content = %#v, want empty", textOnly[0].UserInputMultiContent)
	}
}

func TestToEinoMessagesMapsRolesAndToolCalls(t *testing.T) {
	got := ToEinoMessages([]types.Message{
		{Role: types.RoleSystem, Content: "system"},
		{Role: types.RoleDeveloper, Content: "developer"},
		{
			Role:    types.RoleAssistant,
			Content: "assistant",
			ToolCalls: []types.ToolCall{{
				ID:       "tool-1",
				Type:     types.ToolCallTypeFunction,
				Function: types.FunctionCall{Name: "file_read", Arguments: `{"path":"README.md"}`},
			}},
		},
		{Role: types.RoleTool, Content: "result", ToolCallID: "tool-1"},
		{Role: types.RoleReasoning, Content: "skip reasoning"},
		{Role: types.RoleActivity, Content: "skip activity"},
	})

	if len(got) != 4 {
		t.Fatalf("messages len = %d, want 4", len(got))
	}
	if got[0].Role != schema.System || got[0].Content != "system" {
		t.Fatalf("system message = %#v", got[0])
	}
	if got[1].Role != schema.System || got[1].Content != "developer" {
		t.Fatalf("developer message = %#v", got[1])
	}
	if got[2].Role != schema.Assistant || got[2].Content != "assistant" {
		t.Fatalf("assistant message = %#v", got[2])
	}
	if got[2].ToolCalls[0].ID != "tool-1" || got[2].ToolCalls[0].Function.Name != "file_read" {
		t.Fatalf("assistant tool calls = %#v", got[2].ToolCalls)
	}
	if got[3].Role != schema.Tool || got[3].ToolCallID != "tool-1" || got[3].Content != "result" {
		t.Fatalf("tool message = %#v", got[3])
	}
}

func TestMessageTextJoinsTextFragments(t *testing.T) {
	got := MessageText(types.Message{Role: types.RoleUser, Content: []types.InputContent{
		{Type: types.InputContentTypeText, Text: "First"},
		{Type: types.InputContentTypeImage, URL: "https://example.test/cat.png"},
		{Type: types.InputContentTypeText, Text: "Second"},
	}})
	if want := "First\nSecond"; got != want {
		t.Fatalf("MessageText = %q, want %q", got, want)
	}
}

func TestToEinoImagePartSourceVariants(t *testing.T) {
	urlPart, ok := ToEinoImagePart(types.InputContent{
		Type: types.InputContentTypeImage,
		Source: &types.InputContentSource{
			Type:  types.InputContentSourceTypeURL,
			Value: "https://example.test/cat.png",
		},
	})
	if !ok || urlPart.Image == nil || urlPart.Image.URL == nil || *urlPart.Image.URL != "https://example.test/cat.png" {
		t.Fatalf("URL image part = %#v, %v", urlPart, ok)
	}

	dataPart, ok := ToEinoImagePart(types.InputContent{
		Type: types.InputContentTypeImage,
		Source: &types.InputContentSource{
			Type:     types.InputContentSourceTypeData,
			Value:    "base64",
			MimeType: "image/png",
		},
	})
	if !ok || dataPart.Image == nil || dataPart.Image.Base64Data == nil || *dataPart.Image.Base64Data != "base64" || dataPart.Image.MIMEType != "image/png" {
		t.Fatalf("data image part = %#v, %v", dataPart, ok)
	}

	if _, ok := ToEinoImagePart(types.InputContent{Type: types.InputContentTypeImage}); ok {
		t.Fatal("empty image part converted, want dropped")
	}
}

func TestToAGUIMessagesMapsRolesAndIDs(t *testing.T) {
	testids.WithDeterministicGenerator(t, "convert")

	got := ToAGUIMessages([]*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("user"),
		{
			Role:    schema.Assistant,
			Content: "assistant",
			ToolCalls: []schema.ToolCall{{
				ID:       "tool-1",
				Type:     "function",
				Function: schema.FunctionCall{Name: "file_read", Arguments: "{}"},
			}},
		},
		schema.ToolMessage("result", "tool-1"),
		{Role: schema.RoleType("unknown"), Content: "skip"},
	})

	wantRoles := []types.Role{types.RoleSystem, types.RoleUser, types.RoleAssistant, types.RoleTool}
	if gotRoles := rolesOf(got); !reflect.DeepEqual(gotRoles, wantRoles) {
		t.Fatalf("roles = %v, want %v", gotRoles, wantRoles)
	}
	if gotIDs := idsOf(got); !reflect.DeepEqual(gotIDs, []string{"convert-msg-000001", "convert-msg-000002", "convert-msg-000003", "convert-msg-000004"}) {
		t.Fatalf("ids = %v", gotIDs)
	}
	if got[2].ToolCalls[0].Type != types.ToolCallTypeFunction || got[2].ToolCalls[0].Function.Name != "file_read" {
		t.Fatalf("assistant tool calls = %#v", got[2].ToolCalls)
	}
	if got[3].ToolCallID != "tool-1" {
		t.Fatalf("toolCallID = %q, want tool-1", got[3].ToolCallID)
	}
}

func TestToolCallConversionSkipsEmptyIDs(t *testing.T) {
	einoCalls := ToEinoToolCalls([]types.ToolCall{
		{ID: "", Function: types.FunctionCall{Name: "bad", Arguments: "{}"}},
		{ID: "tool-1", Function: types.FunctionCall{Name: "good", Arguments: "{}"}},
	})
	if len(einoCalls) != 1 {
		t.Fatalf("eino tool calls len = %d, want 1", len(einoCalls))
	}
	if einoCalls[0].ID != "tool-1" || einoCalls[0].Function.Name != "good" {
		t.Fatalf("eino tool call = %#v", einoCalls[0])
	}
	if got := ToEinoToolCalls([]types.ToolCall{{ID: ""}}); got != nil {
		t.Fatalf("empty-ID AG-UI calls converted to %#v, want nil", got)
	}

	aguiCalls := ToAGUIToolCalls([]schema.ToolCall{
		{ID: "", Function: schema.FunctionCall{Name: "bad", Arguments: "{}"}},
		{ID: "tool-2", Function: schema.FunctionCall{Name: "good", Arguments: "{}"}},
	})
	if len(aguiCalls) != 1 {
		t.Fatalf("AG-UI tool calls len = %d, want 1", len(aguiCalls))
	}
	if aguiCalls[0].ID != "tool-2" || aguiCalls[0].Function.Name != "good" {
		t.Fatalf("AG-UI tool call = %#v", aguiCalls[0])
	}
	if got := ToAGUIToolCalls([]schema.ToolCall{{ID: ""}}); got != nil {
		t.Fatalf("empty-ID eino calls converted to %#v, want nil", got)
	}
}

func rolesOf(messages []types.Message) []types.Role {
	roles := make([]types.Role, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.Role)
	}
	return roles
}

func idsOf(messages []types.Message) []string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	return ids
}
