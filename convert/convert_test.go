package convert

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/internal/testids"
)

func TestToEinoMessagesMatchesNormalizedGoldenFixture(t *testing.T) {
	fixture := readConvertFixture(t)
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture cases are empty")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			opts := []EinoOption{}
			if tc.Input.Provider == "openai" {
				opts = append(opts, WithVisionSupport(true))
			}

			gotMessages := ToEinoMessages([]types.Message{tc.Input.AGUIMessage}, opts...)
			if len(gotMessages) != 1 {
				t.Fatalf("messages len = %d, want 1", len(gotMessages))
			}
			if got := normalizeEinoMessage(gotMessages[0]); !reflect.DeepEqual(got, tc.Normalized) {
				t.Fatalf("normalized message = %#v, want %#v", got, tc.Normalized)
			}
		})
	}
}

func TestMessageTextMatchesNormalizedGoldenFixture(t *testing.T) {
	fixture := readConvertFixture(t)
	if len(fixture.MessageTextCases) == 0 {
		t.Fatal("fixture messageTextCases are empty")
	}
	for _, tc := range fixture.MessageTextCases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := MessageText(tc.Input); got != tc.Normalized {
				t.Fatalf("MessageText = %q, want %q", got, tc.Normalized)
			}
		})
	}
}

func TestToEinoImagePartMatchesNormalizedGoldenFixture(t *testing.T) {
	fixture := readConvertFixture(t)
	if len(fixture.ImagePartCases) == 0 {
		t.Fatal("fixture imagePartCases are empty")
	}
	for _, tc := range fixture.ImagePartCases {
		t.Run(tc.Name, func(t *testing.T) {
			got, ok := ToEinoImagePart(tc.Input)
			if !ok {
				t.Fatal("ToEinoImagePart ok = false, want true")
			}
			if normalized := normalizeInputPart(got); !reflect.DeepEqual(normalized, tc.Normalized) {
				t.Fatalf("normalized image part = %#v, want %#v", normalized, tc.Normalized)
			}
		})
	}
}

func TestToolCallsMatchNormalizedGoldenFixture(t *testing.T) {
	fixture := readConvertFixture(t)
	if len(fixture.ToolCalls.AGUI) == 0 || len(fixture.ToolCalls.Eino) == 0 {
		t.Fatal("fixture toolCalls are empty")
	}

	einoCalls := ToEinoToolCalls(fixture.ToolCalls.AGUI)
	if got, want := normalizeEinoToolCalls(einoCalls), normalizeAGUIToolCalls(fixture.ToolCalls.AGUI); !reflect.DeepEqual(got, want) {
		t.Fatalf("ToEinoToolCalls = %#v, want %#v", got, want)
	}

	aguiCalls := ToAGUIToolCalls(schemaToolCallsFromFixture(fixture.ToolCalls.Eino))
	if got, want := normalizeAGUIToolCalls(aguiCalls), fixture.ToolCalls.Eino; !reflect.DeepEqual(got, want) {
		t.Fatalf("ToAGUIToolCalls = %#v, want %#v", got, want)
	}
}

func TestToAGUIMessagesMatchesNormalizedGoldenFixture(t *testing.T) {
	fixture := readConvertFixture(t)
	if len(fixture.AGUIMessages.Input) == 0 || len(fixture.AGUIMessages.Normalized) == 0 {
		t.Fatal("fixture aguiMessages are empty")
	}
	testids.WithDeterministicGenerator(t, "convert")

	got := ToAGUIMessages(schemaMessagesFromFixture(fixture.AGUIMessages.Input))
	if normalized := normalizeAGUIMessages(got); !reflect.DeepEqual(normalized, fixture.AGUIMessages.Normalized) {
		t.Fatalf("normalized AG-UI messages = %#v, want %#v", normalized, fixture.AGUIMessages.Normalized)
	}
}

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
	if got[2].ToolCalls[0].ID != "tool-1" || got[2].ToolCalls[0].Function.Name != "file_read" || got[2].ToolCalls[0].Function.Arguments != `{"path":"README.md"}` {
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

	legacyURLPart, ok := ToEinoImagePart(types.InputContent{
		Type: types.InputContentTypeImage,
		URL:  "https://example.test/legacy.png",
	})
	if !ok || legacyURLPart.Image == nil || legacyURLPart.Image.URL == nil || *legacyURLPart.Image.URL != "https://example.test/legacy.png" {
		t.Fatalf("legacy URL image part = %#v, %v", legacyURLPart, ok)
	}

	legacyDataPart, ok := ToEinoImagePart(types.InputContent{
		Type:     types.InputContentTypeImage,
		Data:     "legacy-base64",
		MimeType: "image/jpeg",
	})
	if !ok || legacyDataPart.Image == nil || legacyDataPart.Image.Base64Data == nil || *legacyDataPart.Image.Base64Data != "legacy-base64" || legacyDataPart.Image.MIMEType != "image/jpeg" {
		t.Fatalf("legacy data image part = %#v, %v", legacyDataPart, ok)
	}

	if _, ok := ToEinoImagePart(types.InputContent{Type: types.InputContentTypeImage}); ok {
		t.Fatal("empty image part converted, want dropped")
	}
}

func TestConvertMalformedInputs(t *testing.T) {
	t.Run("user message drops unusable multimodal content", func(t *testing.T) {
		got := ToEinoUserMessage(types.Message{Role: types.RoleUser, Content: []types.InputContent{
			{Type: types.InputContentTypeText},
			{Type: types.InputContentTypeImage},
		}})
		if got != nil {
			t.Fatalf("ToEinoUserMessage = %#v, want nil", got)
		}
	})

	t.Run("unknown image source is rejected", func(t *testing.T) {
		got, ok := ToEinoImagePart(types.InputContent{
			Type: types.InputContentTypeImage,
			Source: &types.InputContentSource{
				Type:  "unknown",
				Value: "ignored",
			},
		})
		if ok || !reflect.DeepEqual(got, schema.MessageInputPart{}) {
			t.Fatalf("ToEinoImagePart = %#v, %v; want zero,false", got, ok)
		}
	})

	t.Run("empty text-only user message is skipped", func(t *testing.T) {
		if got := ToEinoMessages([]types.Message{{Role: types.RoleUser, Content: ""}}); len(got) != 0 {
			t.Fatalf("ToEinoMessages = %#v, want empty", got)
		}
	})
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
	if gotContents := contentsOf(got); !reflect.DeepEqual(gotContents, []string{"system", "user", "assistant", "result"}) {
		t.Fatalf("contents = %v", gotContents)
	}
	if got[2].ToolCalls[0].Type != types.ToolCallTypeFunction || got[2].ToolCalls[0].Function.Name != "file_read" || got[2].ToolCalls[0].Function.Arguments != "{}" {
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
	if einoCalls[0].Function.Arguments != "{}" {
		t.Fatalf("eino tool call arguments = %q, want {}", einoCalls[0].Function.Arguments)
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
	if aguiCalls[0].Function.Arguments != "{}" {
		t.Fatalf("AG-UI tool call arguments = %q, want {}", aguiCalls[0].Function.Arguments)
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

func contentsOf(messages []types.Message) []string {
	contents := make([]string, 0, len(messages))
	for _, message := range messages {
		content, _ := message.ContentString()
		contents = append(contents, content)
	}
	return contents
}

func readConvertFixture(t *testing.T) convertFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/golden/convert.normalized.json")
	if err != nil {
		t.Fatalf("read convert fixture: %v", err)
	}
	var fixture convertFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode convert fixture: %v", err)
	}
	return fixture
}

func normalizeEinoMessage(message *schema.Message) normalizedEinoMessage {
	out := normalizedEinoMessage{
		EinoRole:   string(message.Role),
		Content:    message.Content,
		ToolCalls:  normalizeEinoToolCalls(message.ToolCalls),
		ToolCallID: message.ToolCallID,
	}
	if len(message.UserInputMultiContent) > 0 {
		out.UserInputMultiContent = make([]normalizedInputPart, 0, len(message.UserInputMultiContent))
		for _, part := range message.UserInputMultiContent {
			out.UserInputMultiContent = append(out.UserInputMultiContent, normalizeInputPart(part))
		}
	}
	return out
}

func normalizeInputPart(part schema.MessageInputPart) normalizedInputPart {
	out := normalizedInputPart{Type: string(part.Type), Text: part.Text}
	if part.Image != nil {
		out.Image = &normalizedImage{MIMEType: part.Image.MIMEType}
		if part.Image.URL != nil {
			out.Image.URL = *part.Image.URL
		}
		if part.Image.Base64Data != nil {
			out.Image.Base64Data = *part.Image.Base64Data
		}
	}
	return out
}

func normalizeEinoToolCalls(calls []schema.ToolCall) []normalizedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]normalizedToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, normalizedToolCall{
			ID:   call.ID,
			Type: call.Type,
			Function: normalizedFunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

func normalizeAGUIToolCalls(calls []types.ToolCall) []normalizedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]normalizedToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, normalizedToolCall{
			ID:   call.ID,
			Type: call.Type,
			Function: normalizedFunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

func normalizeAGUIMessages(messages []types.Message) []normalizedAGUIMessage {
	out := make([]normalizedAGUIMessage, 0, len(messages))
	for _, message := range messages {
		content, _ := message.ContentString()
		out = append(out, normalizedAGUIMessage{
			ID:         "<message-id>",
			Role:       string(message.Role),
			Content:    content,
			ToolCalls:  normalizeAGUIToolCalls(message.ToolCalls),
			ToolCallID: message.ToolCallID,
		})
	}
	return out
}

func schemaToolCallsFromFixture(calls []normalizedToolCall) []schema.ToolCall {
	out := make([]schema.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, schema.ToolCall{
			ID:   call.ID,
			Type: call.Type,
			Function: schema.FunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

func schemaMessagesFromFixture(messages []normalizedEinoMessage) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, &schema.Message{
			Role:       schema.RoleType(message.EinoRole),
			Content:    message.Content,
			ToolCalls:  schemaToolCallsFromFixture(message.ToolCalls),
			ToolCallID: message.ToolCallID,
		})
	}
	return out
}

type convertFixture struct {
	Cases            []convertFixtureCase     `json:"cases"`
	MessageTextCases []messageTextFixtureCase `json:"messageTextCases"`
	ImagePartCases   []imagePartFixtureCase   `json:"imagePartCases"`
	ToolCalls        toolCallsFixture         `json:"toolCalls"`
	AGUIMessages     aguiMessagesFixture      `json:"aguiMessages"`
}

type convertFixtureCase struct {
	Name  string `json:"name"`
	Input struct {
		Provider    string        `json:"provider"`
		AGUIMessage types.Message `json:"aguiMessage"`
	} `json:"input"`
	Normalized normalizedEinoMessage `json:"normalized"`
}

type messageTextFixtureCase struct {
	Name       string        `json:"name"`
	Input      types.Message `json:"input"`
	Normalized string        `json:"normalized"`
}

type imagePartFixtureCase struct {
	Name       string              `json:"name"`
	Input      types.InputContent  `json:"input"`
	Normalized normalizedInputPart `json:"normalized"`
}

type toolCallsFixture struct {
	AGUI []types.ToolCall     `json:"agui"`
	Eino []normalizedToolCall `json:"eino"`
}

type aguiMessagesFixture struct {
	Input      []normalizedEinoMessage `json:"input"`
	Normalized []normalizedAGUIMessage `json:"normalized"`
}

type normalizedEinoMessage struct {
	EinoRole              string                `json:"einoRole"`
	Content               string                `json:"content"`
	UserInputMultiContent []normalizedInputPart `json:"userInputMultiContent"`
	ToolCalls             []normalizedToolCall  `json:"toolCalls"`
	ToolCallID            string                `json:"toolCallId"`
}

type normalizedInputPart struct {
	Type  string           `json:"type"`
	Text  string           `json:"text"`
	Image *normalizedImage `json:"image"`
}

type normalizedImage struct {
	URL        string `json:"url"`
	Base64Data string `json:"base64Data"`
	MIMEType   string `json:"mimeType"`
}

type normalizedToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function normalizedFunctionCall `json:"function"`
}

type normalizedFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type normalizedAGUIMessage struct {
	ID         string               `json:"id"`
	Role       string               `json:"role"`
	Content    string               `json:"content"`
	ToolCalls  []normalizedToolCall `json:"toolCalls"`
	ToolCallID string               `json:"toolCallId"`
}
