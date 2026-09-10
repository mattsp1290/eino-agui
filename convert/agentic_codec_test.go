package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino/schema/claude"
	"github.com/cloudwego/eino/schema/gemini"
	"github.com/cloudwego/eino/schema/openai"
	jsonschema "github.com/eino-contrib/jsonschema"
)

func testIdentity() AgenticIdentityV1 {
	return AgenticIdentityV1{
		SessionID: "session-1", ThreadID: "session-1", RunID: "run-1",
		TurnID: "turn-1", MessageID: "message-1", AttemptID: "attempt-1",
		AgentPath: []AgentPathSegment{{Name: "root", RunID: "run-1"}},
	}
}

func TestAgenticProjectionBuildsNativeUserInputWithoutPrivateFields(t *testing.T) {
	t.Parallel()
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.UserInputText{Text: "hello"}),
		schema.NewContentBlock(&schema.UserInputImage{URL: "https://example.test/image.png", MIMEType: "image/png", Detail: schema.ImageURLDetailHigh}),
		schema.NewContentBlock(&schema.UserInputFile{Base64Data: "cGRm", MIMEType: "application/pdf", Name: "public.pdf"}),
	}}
	projection, err := ToAgenticProjection(message, AgenticProjectionContext{
		Identity: testIdentity(),
		Blocks:   []AgenticBlockContext{{BlockID: "text"}, {BlockID: "image"}, {BlockID: "file"}},
		Limits:   DefaultProjectionLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if projection.NativeMessage == nil || projection.NativeMessage.ID != testIdentity().MessageID {
		t.Fatalf("native message = %#v", projection.NativeMessage)
	}
	parts, ok := projection.NativeMessage.Content.([]types.InputContent)
	if !ok || len(parts) != 3 {
		t.Fatalf("native content = %#v", projection.NativeMessage.Content)
	}
	if parts[0].Type != types.InputContentTypeText || parts[0].Text != "hello" {
		t.Fatalf("text part = %#v", parts[0])
	}
	if parts[1].Source == nil || parts[1].Source.Type != types.InputContentSourceTypeURL {
		t.Fatalf("image part = %#v", parts[1])
	}
	filename, _ := parts[2].Metadata.(map[string]any)["filename"].(string)
	if parts[2].Source == nil || parts[2].Source.Type != types.InputContentSourceTypeData || filename != "public.pdf" {
		t.Fatalf("file part = %#v", parts[2])
	}
	wire, err := json.Marshal(projection.NativeMessage)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"signature", "encrypted", "extra"} {
		if bytes.Contains(bytes.ToLower(wire), []byte(private)) {
			t.Fatalf("native message leaked %q: %s", private, wire)
		}
	}
}

func testContext(blocks ...AgenticBlockContext) AgenticProjectionContext {
	return AgenticProjectionContext{Identity: testIdentity(), Blocks: blocks, Limits: DefaultProjectionLimits()}
}

func TestProjectAgenticMessageAllContentKinds(t *testing.T) {
	t.Parallel()
	code := int64(7)
	cases := []struct {
		name    string
		role    schema.AgenticRoleType
		block   *schema.ContentBlock
		context AgenticBlockContext
	}{
		{"reasoning", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.Reasoning{Text: "why", Signature: "PRIVATE"}), AgenticBlockContext{BlockID: "b"}},
		{"user text", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputText{Text: "hello"}), AgenticBlockContext{BlockID: "b"}},
		{"user image", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputImage{URL: "https://example/image", MIMEType: "image/png"}), AgenticBlockContext{BlockID: "b"}},
		{"user audio", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputAudio{Base64Data: "YWJj", MIMEType: "audio/wav"}), AgenticBlockContext{BlockID: "b"}},
		{"user video", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputVideo{URL: "https://example/video", MIMEType: "video/mp4"}), AgenticBlockContext{BlockID: "b"}},
		{"user file", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputFile{Base64Data: "YWJj", Name: "a.pdf", MIMEType: "application/pdf"}), AgenticBlockContext{BlockID: "b"}},
		{"tool search", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search-1", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{{Name: "found", Desc: "found tool"}}}}), AgenticBlockContext{BlockID: "b"}},
		{"assistant text", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenText{Text: "answer"}), AgenticBlockContext{BlockID: "b"}},
		{"assistant image", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenImage{URL: "https://example/generated", MIMEType: "image/png"}), AgenticBlockContext{BlockID: "b"}},
		{"assistant audio", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenAudio{Base64Data: "YWJj", MIMEType: "audio/wav"}), AgenticBlockContext{BlockID: "b"}},
		{"assistant video", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenVideo{URL: "https://example/generated-video", MIMEType: "video/mp4"}), AgenticBlockContext{BlockID: "b"}},
		{"function call", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call-1", Name: "fn", Arguments: `{}`}), AgenticBlockContext{BlockID: "b"}},
		{"function result", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.FunctionToolResult{CallID: "call-1", Name: "fn", Content: []*schema.FunctionToolResultContentBlock{{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "ok"}}}}), AgenticBlockContext{BlockID: "b"}},
		{"server call", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.ServerToolCall{CallID: "server-call", Name: "web", Arguments: map[string]any{"q": "go"}}), AgenticBlockContext{BlockID: "b", ProviderServerID: "provider-server"}},
		{"server result", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.ServerToolResult{CallID: "server-call", Name: "web", Content: []any{"result"}}), AgenticBlockContext{BlockID: "b", ProviderServerID: "provider-server"}},
		{"mcp call", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPToolCall{ServerLabel: "server", ApprovalRequestID: "approval", CallID: "mcp-call", Name: "mcp", Arguments: `{}`}), AgenticBlockContext{BlockID: "b"}},
		{"mcp result", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPToolResult{ServerLabel: "server", CallID: "mcp-call", Name: "mcp", Content: `{}`, Error: &schema.MCPToolCallError{Code: &code, Message: "failed"}}), AgenticBlockContext{BlockID: "b"}},
		{"mcp list", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{{Name: "mcp", Description: "tool"}}}), AgenticBlockContext{BlockID: "b"}},
		{"mcp approval request", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPToolApprovalRequest{ID: "approval", ServerLabel: "server", Name: "mcp", Arguments: `{}`}), AgenticBlockContext{BlockID: "b"}},
		{"mcp approval response", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.MCPToolApprovalResponse{ApprovalRequestID: "approval", Approve: true, Reason: "yes"}), AgenticBlockContext{BlockID: "b", ExpectedApprovalRequestID: "approval"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			message := &schema.AgenticMessage{Role: tc.role, ContentBlocks: []*schema.ContentBlock{tc.block}}
			message.Extra = map[string]any{"private": "PRIVATE"}
			got, err := ProjectAgenticMessage(message, testContext(tc.context))
			if err != nil {
				t.Fatalf("ProjectAgenticMessage() error = %v", err)
			}
			if got.ContentBlocks[0].Type != tc.block.Type {
				t.Fatalf("type = %q, want %q", got.ContentBlocks[0].Type, tc.block.Type)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "PRIVATE") {
				t.Fatalf("private sentinel leaked: %s", encoded)
			}
		})
	}
}

func TestProjectFunctionResultAllNestedKinds(t *testing.T) {
	t.Parallel()
	parts := []*schema.FunctionToolResultContentBlock{
		{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "text"}},
		{Type: schema.FunctionToolResultContentBlockTypeImage, Image: &schema.UserInputImage{URL: "https://example/image"}},
		{Type: schema.FunctionToolResultContentBlockTypeAudio, Audio: &schema.UserInputAudio{URL: "https://example/audio"}},
		{Type: schema.FunctionToolResultContentBlockTypeVideo, Video: &schema.UserInputVideo{URL: "https://example/video"}},
		{Type: schema.FunctionToolResultContentBlockTypeFile, File: &schema.UserInputFile{URL: "https://example/file", Name: "f"}},
	}
	msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolResult{CallID: "call", Name: "fn", Content: parts})}}
	got, err := ProjectAgenticMessage(msg, testContext(AgenticBlockContext{BlockID: "block"}))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.ContentBlocks[0].FunctionToolResult.Content); n != 5 {
		t.Fatalf("nested parts = %d, want 5", n)
	}
}

func TestProjectRejectsMalformedUnionBeforeReturningProjection(t *testing.T) {
	t.Parallel()
	bad := &schema.ContentBlock{Type: schema.ContentBlockTypeReasoning, Reasoning: &schema.Reasoning{Text: "x"}, AssistantGenText: &schema.AssistantGenText{Text: "y"}}
	_, err := ProjectAgenticMessage(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{bad}}, testContext(AgenticBlockContext{BlockID: "block"}))
	if err == nil || !strings.Contains(err.Error(), "union") {
		t.Fatalf("error = %v, want union error", err)
	}
}

func TestProjectionPrivacyAndImmutability(t *testing.T) {
	t.Parallel()
	msg := &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "hello", ClaudeExtension: &claude.AssistantGenTextExtension{Citations: []*claude.TextCitation{{Type: claude.TextCitationTypeWebSearchResultLocation, WebSearchResultLocation: &claude.CitationWebSearchResultLocation{Title: "source", URL: "https://example", EncryptedIndex: "PRIVATE_ENCRYPTED"}}}}})},
		ResponseMeta:  &schema.AgenticResponseMeta{OpenAIExtension: &openai.ResponseMetaExtension{ID: "PRIVATE_RESPONSE_ID"}, GeminiExtension: &gemini.ResponseMetaExtension{ID: "PRIVATE_GEMINI_ID", GroundingMeta: &gemini.GroundingMetadata{SearchEntryPoint: &gemini.SearchEntryPoint{SDKBlob: []byte("PRIVATE_BLOB")}}}},
		Extra:         map[string]any{"private": "PRIVATE_EXTRA"},
	}
	got, err := ProjectAgenticMessage(msg, testContext(AgenticBlockContext{BlockID: "block"}))
	if err != nil {
		t.Fatal(err)
	}
	msg.ContentBlocks[0].AssistantGenText.Text = "mutated"
	if *got.ContentBlocks[0].Text != "hello" {
		t.Fatal("projection aliases input")
	}
	b, _ := json.Marshal(got)
	for _, sentinel := range []string{"PRIVATE_ENCRYPTED", "PRIVATE_RESPONSE_ID", "PRIVATE_GEMINI_ID", "PRIVATE_BLOB", "PRIVATE_EXTRA"} {
		if strings.Contains(string(b), sentinel) {
			t.Fatalf("sentinel %q leaked", sentinel)
		}
	}
}

func TestEnvelopeStrictDecodeAndDigest(t *testing.T) {
	t.Parallel()
	envelope := &AgenticEnvelopeV1{Version: 1, Kind: EnvelopeTurnFinished, Identity: testIdentity(), Lifecycle: &LifecycleFactV1{Detail: "committed"}}
	digest, err := LifecycleDigestV1(envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Digest = digest
	b, _ := json.Marshal(envelope)
	if _, err := DecodeAgenticEnvelope(b); err != nil {
		t.Fatalf("DecodeAgenticEnvelope() error = %v", err)
	}
	var object map[string]any
	_ = json.Unmarshal(b, &object)
	object["unknown"] = true
	if _, err := DecodeAgenticEnvelope(object); err == nil {
		t.Fatal("unknown member was accepted")
	}
	envelope.Lifecycle.Detail = "altered"
	if err := ValidateAgenticEnvelope(envelope); err == nil {
		t.Fatal("altered digest was accepted")
	}
}

func TestEnvelopeStrictDecodeRejectsAmbiguousContentUnion(t *testing.T) {
	t.Parallel()
	projection, err := ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "hello"})}},
		testContext(AgenticBlockContext{BlockID: "block"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	envelope := projection.Blocks[0].Supplement
	envelope.ContentBlock.Media = &PublicMedia{URL: "https://example.test/image.png"}
	envelope.Digest = CandidateDigestV1(strings.Repeat("0", 64))
	if _, err := DecodeAgenticEnvelope(&envelope); err == nil {
		t.Fatal("ambiguous nested content union was accepted")
	}
}

func TestAttemptReplacementRequiresCauseAndSemantics(t *testing.T) {
	t.Parallel()
	id := testIdentity()
	for _, replacement := range []AttemptReplacedV1{
		{OldAttemptID: "old", NewAttemptID: "new", Semantics: "replace"},
		{OldAttemptID: "old", NewAttemptID: "new", Cause: "retry"},
	} {
		envelope := &AgenticEnvelopeV1{Version: AgenticSchemaVersion, Kind: EnvelopeAttemptReplaced, Identity: id, AttemptReplaced: &replacement}
		if _, err := LifecycleDigestV1(envelope); err == nil {
			t.Fatalf("replacement %#v unexpectedly validated", replacement)
		}
	}
}

func TestServerJSONRejectsCyclesAndNonFiniteNumbers(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	for _, value := range []any{cyclic, map[string]any{"bad": json.Number("NaN")}} {
		msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.ServerToolCall{CallID: "call", Name: "server", Arguments: value})}}
		_, err := ProjectAgenticMessage(msg, testContext(AgenticBlockContext{BlockID: "block", ProviderServerID: "provider"}))
		if err == nil {
			t.Fatal("invalid server value was accepted")
		}
	}
}

func TestToolDefinitionPreservesParameterRepresentation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tool *schema.ToolInfo
		want string
	}{
		{"none", &schema.ToolInfo{Name: "none"}, "none"},
		{"present empty params", &schema.ToolInfo{Name: "params", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, "params"},
		{"structured params", &schema.ToolInfo{Name: "structured", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"query": {Type: schema.String, Required: true}})}, "params"},
		{"json schema", &schema.ToolInfo{Name: "json", ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&jsonschema.Schema{Type: "object"})}, "json_schema"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projected, err := projectToolDefinition(tc.tool)
			if err != nil {
				t.Fatal(err)
			}
			if projected.ParamsKind != tc.want {
				t.Fatalf("paramsKind = %q, want %q", projected.ParamsKind, tc.want)
			}
			if tc.name == "present empty params" && projected.Params == nil {
				t.Fatal("present empty params became nil")
			}
		})
	}
}

func TestProjectionLimitsAndApprovalCorrelation(t *testing.T) {
	t.Parallel()
	limits := DefaultProjectionLimits()
	limits.MaxBlocks = 1
	msg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.UserInputText{Text: "one"}),
		schema.NewContentBlock(&schema.UserInputText{Text: "two"}),
	}}
	ctx := testContext(AgenticBlockContext{BlockID: "one"}, AgenticBlockContext{BlockID: "two"})
	ctx.Limits = limits
	if _, err := ProjectAgenticMessage(msg, ctx); err == nil {
		t.Fatal("one-over block limit was accepted")
	}

	envelope := &AgenticEnvelopeV1{Version: 1, Kind: EnvelopePaused, Identity: testIdentity(), Paused: &PausedV1{
		Targets:     []InterruptTargetV1{{ID: "interrupt", Address: "node/0"}},
		Correlation: &ApprovalInterruptCorrelation{ApprovalRequestID: "approval", InterruptTargetID: "different", InterruptAddress: "node/0"},
	}}
	if _, err := LifecycleDigestV1(envelope); err == nil {
		t.Fatal("unvalidated approval correlation was accepted")
	}
}

func TestFixedDigestVectors(t *testing.T) {
	t.Parallel()
	projection := &PublicAgenticMessage{Version: 1, Identity: testIdentity(), Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []PublicContentBlock{}}
	projectionDigest, err := ProjectionDigestV1(projection)
	if err != nil {
		t.Fatal(err)
	}
	envelope := &AgenticEnvelopeV1{Version: 1, Kind: EnvelopeTurnFinished, Identity: testIdentity(), Lifecycle: &LifecycleFactV1{Detail: "committed"}}
	lifecycleDigest, err := LifecycleDigestV1(envelope)
	if err != nil {
		t.Fatal(err)
	}
	const wantProjection = "1278a78185c973ceee246c84bc2f69b40632853cc428cef3094f4c099eb93c47"
	const wantLifecycle = "50a2df185754f533c91bce45deb459b93029c77ada20baf50aee0a96aa908d00"
	if projectionDigest != wantProjection {
		t.Fatalf("projection digest = %s", projectionDigest)
	}
	if lifecycleDigest != wantLifecycle {
		t.Fatalf("lifecycle digest = %s", lifecycleDigest)
	}
}

func FuzzDecodeAgenticEnvelope(f *testing.F) {
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic: %v", r)
			}
		}()
		_, first := DecodeAgenticEnvelope(data)
		_, second := DecodeAgenticEnvelope(data)
		if (first == nil) != (second == nil) {
			t.Fatal(errors.New("nondeterministic decode result"))
		}
	})
}
