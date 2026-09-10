package convert

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func TestProviderToolExecutionOwnersAreExplicitAndStrict(t *testing.T) {
	t.Parallel()
	code := int64(7)
	tests := []struct {
		name    string
		block   *schema.ContentBlock
		context AgenticBlockContext
		owner   func(PublicContentBlock) ToolExecutionOwner
	}{
		{
			name: "server call", block: schema.NewContentBlock(&schema.ServerToolCall{CallID: "server-call", Name: "web", Arguments: map[string]any{"q": "go"}}),
			context: AgenticBlockContext{BlockID: "block", ProviderServerID: "provider-server"}, owner: func(block PublicContentBlock) ToolExecutionOwner { return block.ServerToolCall.ExecutionOwner },
		},
		{
			name: "server result", block: schema.NewContentBlock(&schema.ServerToolResult{CallID: "server-call", Name: "web", Content: []any{"result"}}),
			context: AgenticBlockContext{BlockID: "block", ProviderServerID: "provider-server"}, owner: func(block PublicContentBlock) ToolExecutionOwner { return block.ServerToolResult.ExecutionOwner },
		},
		{
			name: "MCP call", block: schema.NewContentBlock(&schema.MCPToolCall{ServerLabel: "server", CallID: "mcp-call", Name: "lookup", Arguments: `{}`}),
			context: AgenticBlockContext{BlockID: "block"}, owner: func(block PublicContentBlock) ToolExecutionOwner { return block.MCPToolCall.ExecutionOwner },
		},
		{
			name: "MCP result", block: schema.NewContentBlock(&schema.MCPToolResult{ServerLabel: "server", CallID: "mcp-call", Name: "lookup", Content: `{}`, Error: &schema.MCPToolCallError{Code: &code, Message: "failed"}}),
			context: AgenticBlockContext{BlockID: "block"}, owner: func(block PublicContentBlock) ToolExecutionOwner { return block.MCPToolResult.ExecutionOwner },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projection, err := ToAgenticProjection(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{tc.block}}, testContext(tc.context))
			if err != nil {
				t.Fatal(err)
			}
			want := ToolExecutionOwnerProvider
			if strings.HasPrefix(tc.name, "MCP") {
				want = ToolExecutionOwnerProviderMCP
			}
			if got := tc.owner(projection.Public.ContentBlocks[0]); got != want {
				t.Fatalf("execution owner = %q, want %q", got, want)
			}
			encoded, err := json.Marshal(projection.Blocks[0].Supplement)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded, []byte(`"executionOwner":"`+string(want)+`"`)) {
				t.Fatalf("wire payload lacks execution owner: %s", encoded)
			}

			block := projection.Blocks[0].Supplement.ContentBlock
			switch {
			case block.ServerToolCall != nil:
				block.ServerToolCall.ExecutionOwner = ToolExecutionOwnerProviderMCP
			case block.ServerToolResult != nil:
				block.ServerToolResult.ExecutionOwner = ToolExecutionOwnerProviderMCP
			case block.MCPToolCall != nil:
				block.MCPToolCall.ExecutionOwner = ToolExecutionOwnerProvider
			case block.MCPToolResult != nil:
				block.MCPToolResult.ExecutionOwner = ToolExecutionOwnerProvider
			}
			projection.Blocks[0].Supplement.Digest = ""
			if _, err := DecodeAgenticEnvelope(&projection.Blocks[0].Supplement); err == nil {
				t.Fatal("wrong execution owner was accepted")
			}
		})
	}

	local, err := ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolCall{CallID: "local-call", Name: "local", Arguments: `{}`})}},
		testContext(AgenticBlockContext{BlockID: "local-block"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(local.Blocks[0].Supplement)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("executionOwner")) {
		t.Fatalf("host-local function call acquired provider ownership: %s", encoded)
	}
}

func TestDurableCallAndApprovalIdentitiesArePhaseUnique(t *testing.T) {
	t.Parallel()
	project := func(role schema.AgenticRoleType, blocks []*schema.ContentBlock, contexts []AgenticBlockContext) (*PublicAgenticMessage, error) {
		return ProjectAgenticMessage(&schema.AgenticMessage{Role: role, ContentBlocks: blocks}, testContext(contexts...))
	}
	duplicateCases := []struct {
		name     string
		role     schema.AgenticRoleType
		blocks   []*schema.ContentBlock
		contexts []AgenticBlockContext
	}{
		{
			name: "function call proposals", role: schema.AgenticRoleTypeAssistant,
			blocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call", Name: "first", Arguments: `{}`}),
				schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call", Name: "second", Arguments: `{}`}),
			}, contexts: []AgenticBlockContext{{BlockID: "one"}, {BlockID: "two"}},
		},
		{
			name: "server call results", role: schema.AgenticRoleTypeAssistant,
			blocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.ServerToolResult{CallID: "call", Name: "first", Content: "one"}),
				schema.NewContentBlock(&schema.ServerToolResult{CallID: "call", Name: "second", Content: "two"}),
			}, contexts: []AgenticBlockContext{{BlockID: "one", ProviderServerID: "provider"}, {BlockID: "two", ProviderServerID: "provider"}},
		},
		{
			name: "approval requests", role: schema.AgenticRoleTypeAssistant,
			blocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.MCPToolApprovalRequest{ID: "approval", ServerLabel: "server", Name: "first", Arguments: `{}`}),
				schema.NewContentBlock(&schema.MCPToolApprovalRequest{ID: "approval", ServerLabel: "server", Name: "second", Arguments: `{}`}),
			}, contexts: []AgenticBlockContext{{BlockID: "one"}, {BlockID: "two"}},
		},
		{
			name: "approval responses", role: schema.AgenticRoleTypeUser,
			blocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.MCPToolApprovalResponse{ApprovalRequestID: "approval", Approve: true}),
				schema.NewContentBlock(&schema.MCPToolApprovalResponse{ApprovalRequestID: "approval", Approve: false}),
			}, contexts: []AgenticBlockContext{{BlockID: "one", ExpectedApprovalRequestID: "approval"}, {BlockID: "two", ExpectedApprovalRequestID: "approval"}},
		},
	}
	for _, tc := range duplicateCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := project(tc.role, tc.blocks, tc.contexts); err == nil || !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("error=%v, want duplicate identity rejection", err)
			}
		})
	}

	pairedBlocks := []*schema.ContentBlock{
		schema.NewContentBlock(&schema.ServerToolCall{CallID: "server-call", Name: "search", Arguments: map[string]any{"q": "go"}}),
		schema.NewContentBlock(&schema.ServerToolResult{CallID: "server-call", Name: "search", Content: "result"}),
		schema.NewContentBlock(&schema.MCPToolCall{ServerLabel: "server", CallID: "mcp-call", Name: "lookup", Arguments: `{}`}),
		schema.NewContentBlock(&schema.MCPToolResult{ServerLabel: "server", CallID: "mcp-call", Name: "lookup", Content: `{}`}),
	}
	pairedContexts := []AgenticBlockContext{
		{BlockID: "server-call", ProviderServerID: "provider"}, {BlockID: "server-result", ProviderServerID: "provider"},
		{BlockID: "mcp-call"}, {BlockID: "mcp-result"},
	}
	if _, err := project(schema.AgenticRoleTypeAssistant, pairedBlocks, pairedContexts); err != nil {
		t.Fatalf("matching call/result correlations were rejected: %v", err)
	}

	projection, err := project(schema.AgenticRoleTypeAssistant, []*schema.ContentBlock{
		schema.NewContentBlock(&schema.FunctionToolCall{CallID: "first", Name: "lookup", Arguments: `{}`}),
		schema.NewContentBlock(&schema.FunctionToolCall{CallID: "second", Name: "search", Arguments: `{}`}),
	}, []AgenticBlockContext{{BlockID: "first"}, {BlockID: "second"}})
	if err != nil {
		t.Fatal(err)
	}
	projection.ContentBlocks[1].FunctionToolCall.CallID = "first"
	projection.ContentBlocks[1].Identity.CallID = "first"
	if _, err := ProjectionDigestV1(projection); err == nil {
		t.Fatal("caller-mutated duplicate call identity was accepted for digest")
	}
}

func TestProjectRejectsMalformedUnionBeforeReturningProjection(t *testing.T) {
	t.Parallel()
	functionResult := func(part *schema.FunctionToolResultContentBlock) *schema.ContentBlock {
		return schema.NewContentBlock(&schema.FunctionToolResult{CallID: "call", Name: "lookup", Content: []*schema.FunctionToolResultContentBlock{part}})
	}
	tests := []struct {
		name  string
		role  schema.AgenticRoleType
		block *schema.ContentBlock
	}{
		{name: "nil block", role: schema.AgenticRoleTypeAssistant},
		{name: "empty discriminator", role: schema.AgenticRoleTypeAssistant, block: &schema.ContentBlock{}},
		{name: "unknown discriminator", role: schema.AgenticRoleTypeAssistant, block: &schema.ContentBlock{Type: schema.ContentBlockType("unknown")}},
		{name: "nil selected payload", role: schema.AgenticRoleTypeAssistant, block: &schema.ContentBlock{Type: schema.ContentBlockTypeReasoning}},
		{name: "mismatched payload", role: schema.AgenticRoleTypeAssistant, block: &schema.ContentBlock{Type: schema.ContentBlockTypeReasoning, AssistantGenText: &schema.AssistantGenText{Text: "text"}}},
		{name: "ambiguous outer payload", role: schema.AgenticRoleTypeAssistant, block: &schema.ContentBlock{Type: schema.ContentBlockTypeReasoning, Reasoning: &schema.Reasoning{Text: "reason"}, AssistantGenText: &schema.AssistantGenText{Text: "text"}}},
		{name: "nil nested result", role: schema.AgenticRoleTypeUser, block: functionResult(nil)},
		{name: "empty nested discriminator", role: schema.AgenticRoleTypeUser, block: functionResult(&schema.FunctionToolResultContentBlock{})},
		{name: "mismatched nested payload", role: schema.AgenticRoleTypeUser, block: functionResult(&schema.FunctionToolResultContentBlock{Type: schema.FunctionToolResultContentBlockTypeText, Image: &schema.UserInputImage{URL: "https://example.test/image"}})},
		{name: "ambiguous nested payload", role: schema.AgenticRoleTypeUser, block: functionResult(&schema.FunctionToolResultContentBlock{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "text"}, Image: &schema.UserInputImage{URL: "https://example.test/image"}})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projection, err := ProjectAgenticMessage(&schema.AgenticMessage{Role: tc.role, ContentBlocks: []*schema.ContentBlock{tc.block}}, testContext(AgenticBlockContext{BlockID: "block"}))
			if err == nil || projection != nil {
				t.Fatalf("projection=%#v error=%v, want typed rejection", projection, err)
			}
			var projectionError *ProjectionError
			if !errors.As(err, &projectionError) || projectionError.Block != 0 || projectionError.Path == "" {
				t.Fatalf("error = %#v, want block-0 ProjectionError with path", err)
			}
		})
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

func TestForbiddenProviderSentinelsNeverReachProjectionWireOrErrors(t *testing.T) {
	t.Parallel()
	sentinels := []string{
		"SENTINEL_MESSAGE_EXTRA", "SENTINEL_BLOCK_EXTRA", "SENTINEL_SIGNATURE",
		"SENTINEL_REASONING_EXTENSION", "SENTINEL_TEXT_EXTENSION",
		"SENTINEL_CLAUDE_ENCRYPTED_INDEX", "SENTINEL_RESPONSE_EXTENSION",
		"SENTINEL_OPENAI_RESPONSE_ID", "SENTINEL_OPENAI_PREVIOUS_ID",
		"SENTINEL_CLAUDE_RESPONSE_ID", "SENTINEL_GEMINI_RESPONSE_ID",
		"SENTINEL_GEMINI_SDK_BLOB", "SENTINEL_TOOL_EXTRA", "SENTINEL_RESULT_EXTRA",
	}
	reasoning := schema.NewContentBlock(&schema.Reasoning{
		Text: "public reasoning", Signature: sentinels[2],
		OpenAIExtension: &openai.ReasoningExtension{Content: []*openai.ReasoningContent{{Text: sentinels[3]}}},
	})
	reasoning.Extra = map[string]any{"private": sentinels[1]}
	assistantText := schema.NewContentBlock(&schema.AssistantGenText{
		Text: "public answer",
		ClaudeExtension: &claude.AssistantGenTextExtension{Citations: []*claude.TextCitation{{
			Type: claude.TextCitationTypeWebSearchResultLocation,
			WebSearchResultLocation: &claude.CitationWebSearchResultLocation{
				Title: "public title", URL: "https://example.test/source", EncryptedIndex: sentinels[5],
			},
		}}},
		Extension: map[string]any{"private": sentinels[4]},
	})
	assistant := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{reasoning, assistantText},
		Extra: map[string]any{"private": sentinels[0]},
		ResponseMeta: &schema.AgenticResponseMeta{
			Extension: map[string]any{"private": sentinels[6]},
			OpenAIExtension: &openai.ResponseMetaExtension{
				ID: sentinels[7], PreviousResponseID: sentinels[8], Status: openai.ResponseStatus("completed"),
			},
			ClaudeExtension: &claude.ResponseMetaExtension{ID: sentinels[9], StopReason: "end_turn"},
			GeminiExtension: &gemini.ResponseMetaExtension{
				ID: sentinels[10], FinishReason: "STOP",
				GroundingMeta: &gemini.GroundingMetadata{SearchEntryPoint: &gemini.SearchEntryPoint{SDKBlob: []byte(sentinels[11])}},
			},
		},
	}
	assistantProjection, err := ToAgenticProjection(assistant, testContext(
		AgenticBlockContext{BlockID: "reasoning"}, AgenticBlockContext{BlockID: "text"},
	))
	if err != nil {
		t.Fatal(err)
	}

	tool := &schema.ToolInfo{Name: "lookup", Extra: map[string]any{"private": sentinels[12]}}
	resultPart := &schema.FunctionToolResultContentBlock{
		Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "public result"},
		Extra: map[string]any{"private": sentinels[13]},
	}
	user := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{tool}}}),
		schema.NewContentBlock(&schema.FunctionToolResult{CallID: "call", Name: "lookup", Content: []*schema.FunctionToolResultContentBlock{resultPart}}),
	}}
	userProjection, err := ToAgenticProjection(user, testContext(
		AgenticBlockContext{BlockID: "search"}, AgenticBlockContext{BlockID: "result"},
	))
	if err != nil {
		t.Fatal(err)
	}

	var wire [][]byte
	for _, value := range []any{assistantProjection.Public, assistantProjection.Blocks, userProjection.Public, userProjection.Blocks, userProjection.NativeMessage} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		wire = append(wire, encoded)
	}
	for _, projection := range []*AgenticProjection{assistantProjection, userProjection} {
		for _, block := range projection.Blocks {
			for _, event := range block.Native {
				encoded, err := json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				wire = append(wire, encoded)
			}
		}
	}
	for _, encoded := range wire {
		for _, sentinel := range sentinels {
			if bytes.Contains(encoded, []byte(sentinel)) {
				t.Fatalf("private sentinel %q leaked to public wire: %s", sentinel, encoded)
			}
		}
	}

	malformed := *assistantText
	malformed.Type = schema.ContentBlockTypeReasoning
	malformed.Reasoning = &schema.Reasoning{Text: "public", Signature: sentinels[2]}
	_, err = ProjectAgenticMessage(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{&malformed}, Extra: map[string]any{"private": sentinels[0]}},
		testContext(AgenticBlockContext{BlockID: "malformed"}),
	)
	if err == nil {
		t.Fatal("malformed private fixture was accepted")
	}
	for _, sentinel := range sentinels {
		if strings.Contains(err.Error(), sentinel) {
			t.Fatalf("private sentinel %q leaked to error: %v", sentinel, err)
		}
	}
}

func TestProjectionDeeplyDetachesNestedPublicShapes(t *testing.T) {
	t.Parallel()
	snapshot := func(value any) []byte {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	assertUnchanged := func(name string, before []byte, value any) {
		t.Helper()
		after := snapshot(value)
		if !bytes.Equal(before, after) {
			t.Fatalf("%s changed:\nbefore %s\nafter  %s", name, before, after)
		}
	}

	code := int64(9)
	serverArguments := map[string]any{"nested": []any{map[string]any{"value": "original"}}}
	inputSchema := &jsonschema.Schema{Type: "object", Required: []string{"query"}}
	assistant := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "hello", ClaudeExtension: &claude.AssistantGenTextExtension{Citations: []*claude.TextCitation{{
				Type:                    claude.TextCitationTypeWebSearchResultLocation,
				WebSearchResultLocation: &claude.CitationWebSearchResultLocation{CitedText: "hello", Title: "source", URL: "https://example.test/source"},
			}}}}),
			schema.NewContentBlock(&schema.ServerToolCall{CallID: "server", Name: "search", Arguments: serverArguments}),
			schema.NewContentBlock(&schema.MCPToolResult{ServerLabel: "mcp", CallID: "mcp-call", Name: "lookup", Content: `{}`, Error: &schema.MCPToolCallError{Code: &code, Message: "failed"}}),
			schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "mcp", Tools: []*schema.MCPListToolsItem{{Name: "lookup", Description: "lookup", InputSchema: inputSchema}}}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{
			TokenUsage: &schema.TokenUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
			GeminiExtension: &gemini.ResponseMetaExtension{FinishReason: "STOP", GroundingMeta: &gemini.GroundingMetadata{
				GroundingChunks: []*gemini.GroundingChunk{{Web: &gemini.GroundingChunkWeb{Domain: "example.test", Title: "source", URI: "https://example.test/source"}}},
				GroundingSupports: []*gemini.GroundingSupport{{
					ConfidenceScores: []float32{0.75}, GroundingChunkIndices: []int{0},
					Segment: &gemini.Segment{PartIndex: 0, StartIndex: 0, EndIndex: 5, Text: "hello"},
				}},
				WebSearchQueries: []string{"query"},
			}},
		},
	}
	assistantContext := testContext(
		AgenticBlockContext{BlockID: "text"},
		AgenticBlockContext{BlockID: "server", ProviderServerID: "provider"},
		AgenticBlockContext{BlockID: "mcp-result"},
		AgenticBlockContext{BlockID: "mcp-list"},
	)
	assistantInput := struct {
		Message *schema.AgenticMessage
		Context AgenticProjectionContext
	}{assistant, assistantContext}
	assistantBefore := snapshot(assistantInput)
	projection, err := ToAgenticProjection(assistant, assistantContext)
	if err != nil {
		t.Fatal(err)
	}
	public := projection.Public
	public.Identity.AgentPath[0].Name = "mutated-output"
	public.ContentBlocks[0].Identity.AgentPath[0].RunID = "mutated-output"
	*public.ContentBlocks[0].Text = "mutated-output"
	public.ContentBlocks[0].ProviderAnnotations.Claude[0].URL = "https://mutated.invalid"
	public.ContentBlocks[1].ServerToolCall.Arguments.(map[string]any)["nested"].([]any)[0].(map[string]any)["value"] = "mutated-output"
	*public.ContentBlocks[2].MCPToolResult.Error.Code = 10
	public.ContentBlocks[3].MCPListToolsResult.Tools[0].InputSchema[0] = 'X'
	*public.ResponseMeta.TokenUsage.InputTokens = 99
	public.ResponseMeta.GeminiGrounding.Chunks[0].URI = "https://mutated.invalid"
	public.ResponseMeta.GeminiGrounding.Supports[0].ConfidenceScores[0] = 0.1
	public.ResponseMeta.GeminiGrounding.Supports[0].GroundingChunkIndices[0] = 99
	public.ResponseMeta.GeminiGrounding.WebSearchQueries[0] = "mutated-output"
	assertUnchanged("assistant input after output mutation", assistantBefore, assistantInput)

	freshAssistant, err := ToAgenticProjection(assistant, assistantContext)
	if err != nil {
		t.Fatal(err)
	}
	freshAssistantBefore := snapshot(freshAssistant.Public)
	assistant.ContentBlocks[0].AssistantGenText.Text = "mutated-input"
	assistant.ContentBlocks[0].AssistantGenText.ClaudeExtension.Citations[0].WebSearchResultLocation.URL = "https://mutated-input.invalid"
	serverArguments["nested"].([]any)[0].(map[string]any)["value"] = "mutated-input"
	code = 11
	inputSchema.Required[0] = "mutated-input"
	assistant.ResponseMeta.TokenUsage.PromptTokens = 100
	grounding := assistant.ResponseMeta.GeminiExtension.GroundingMeta
	grounding.GroundingChunks[0].Web.URI = "https://mutated-input.invalid"
	grounding.GroundingSupports[0].ConfidenceScores[0] = 0.2
	grounding.GroundingSupports[0].GroundingChunkIndices[0] = 7
	grounding.WebSearchQueries[0] = "mutated-input"
	assistantContext.Identity.AgentPath[0].Name = "mutated-input"
	assertUnchanged("assistant projection after input mutation", freshAssistantBefore, freshAssistant.Public)

	parameter := &schema.ParameterInfo{Type: schema.String, Enum: []string{"one", "two"}, Required: true}
	resultPart := &schema.FunctionToolResultContentBlock{Type: schema.FunctionToolResultContentBlockTypeImage, Image: &schema.UserInputImage{URL: "https://example.test/image", MIMEType: "image/png"}}
	user := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{{Name: "lookup", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"choice": parameter})}}}}),
		schema.NewContentBlock(&schema.FunctionToolResult{CallID: "call", Name: "lookup", Content: []*schema.FunctionToolResultContentBlock{resultPart}}),
	}}
	userContext := testContext(AgenticBlockContext{BlockID: "search"}, AgenticBlockContext{BlockID: "result"})
	userInput := struct {
		Message *schema.AgenticMessage
		Context AgenticProjectionContext
	}{user, userContext}
	userBefore := snapshot(userInput)
	userProjection, err := ToAgenticProjection(user, userContext)
	if err != nil {
		t.Fatal(err)
	}
	userProjection.Public.ContentBlocks[0].ToolSearchResult.Tools[0].Params["choice"].Enum[0] = "mutated-output"
	userProjection.Public.ContentBlocks[0].ToolSearchResult.Tools[0].Params["added"] = &PublicParameterInfo{Type: schema.String}
	userProjection.Public.ContentBlocks[1].FunctionToolResult.Content[0].Media.URL = "https://mutated-output.invalid"
	assertUnchanged("user input after output mutation", userBefore, userInput)

	freshUser, err := ToAgenticProjection(user, userContext)
	if err != nil {
		t.Fatal(err)
	}
	freshUserBefore := snapshot(freshUser.Public)
	parameter.Enum[0] = "mutated-input"
	parameter.Desc = "mutated-input"
	resultPart.Image.URL = "https://mutated-input.invalid"
	userContext.Identity.AgentPath[0].RunID = "mutated-input"
	assertUnchanged("user projection after input mutation", freshUserBefore, freshUser.Public)
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
		{OldAttemptID: testIdentity().AttemptID, NewAttemptID: "new", Semantics: "replace"},
		{OldAttemptID: testIdentity().AttemptID, NewAttemptID: "new", Cause: "retry"},
	} {
		envelope := &AgenticEnvelopeV1{Version: AgenticSchemaVersion, Kind: EnvelopeAttemptReplaced, Identity: id, AttemptReplaced: &replacement}
		if _, err := LifecycleDigestV1(envelope); err == nil {
			t.Fatalf("replacement %#v unexpectedly validated", replacement)
		}
	}
}

func TestCancellationContractRejectsUnknownAndInconsistentValues(t *testing.T) {
	t.Parallel()
	tests := []CancelledV1{
		{RequestedMode: "unknown", ObservedMode: CancellationModeImmediate, Classification: CancellationClassEscalated},
		{RequestedMode: CancellationModeAfterChatModel, ObservedMode: "unknown", Classification: CancellationClassEscalated},
		{RequestedMode: CancellationModeImmediate, ObservedMode: CancellationModeImmediate, Classification: "unknown"},
		{RequestedMode: CancellationModeAfterChatModel, ObservedMode: CancellationModeImmediate, Classification: CancellationClassSafePoint},
		{RequestedMode: CancellationModeImmediate, ObservedMode: CancellationModeImmediate, Classification: CancellationClassTimeout},
		{RequestedMode: CancellationModeAfterToolCalls, ObservedMode: CancellationModeAfterToolCalls, Classification: CancellationClassEscalated},
	}
	for _, cancelled := range tests {
		envelope := &AgenticEnvelopeV1{Version: AgenticSchemaVersion, Kind: EnvelopeCancelled, Identity: testIdentity(), Cancelled: &cancelled}
		if _, err := LifecycleDigestV1(envelope); err == nil {
			t.Fatalf("invalid cancellation was accepted: %#v", cancelled)
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
	privateCycle := map[string]any{}
	privateCycle["PRIVATE_TOOL_EXTRA"] = privateCycle
	cases := []struct {
		name string
		tool *schema.ToolInfo
		want string
	}{
		{"none", &schema.ToolInfo{Name: "none"}, "none"},
		{"present empty params", &schema.ToolInfo{Name: "params", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, "params"},
		{"structured params", &schema.ToolInfo{Name: "structured", Extra: privateCycle, ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"query": {Type: schema.String, Required: true}})}, "params"},
		{"json schema", &schema.ToolInfo{Name: "json", ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&jsonschema.Schema{Type: "object"})}, "json_schema"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projected, err := projectToolDefinition(tc.tool, DefaultProjectionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if projected.ParamsKind != tc.want {
				t.Fatalf("paramsKind = %q, want %q", projected.ParamsKind, tc.want)
			}
			if tc.name == "present empty params" && projected.Params == nil {
				t.Fatal("present empty params became nil")
			}
			encoded, err := json.Marshal(projected)
			if err != nil || bytes.Contains(encoded, []byte("PRIVATE_TOOL_EXTRA")) {
				t.Fatalf("private tool extra leaked or affected encoding: %s / %v", encoded, err)
			}
		})
	}
}

func TestToolDefinitionRejectsInvalidNestedParameterTrees(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		parameter *schema.ParameterInfo
	}{
		{name: "unknown type", parameter: &schema.ParameterInfo{Type: schema.DataType("decimal")}},
		{name: "array with object fields", parameter: &schema.ParameterInfo{Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String}, SubParams: map[string]*schema.ParameterInfo{"value": {Type: schema.String}}}},
		{name: "array with enum", parameter: &schema.ParameterInfo{Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String}, Enum: []string{"value"}}},
		{name: "object with element", parameter: &schema.ParameterInfo{Type: schema.Object, ElemInfo: &schema.ParameterInfo{Type: schema.String}}},
		{name: "object with enum", parameter: &schema.ParameterInfo{Type: schema.Object, Enum: []string{"value"}}},
		{name: "string with child", parameter: &schema.ParameterInfo{Type: schema.String, SubParams: map[string]*schema.ParameterInfo{"value": {Type: schema.String}}}},
		{name: "number with enum", parameter: &schema.ParameterInfo{Type: schema.Number, Enum: []string{"1"}}},
		{name: "nil nested child", parameter: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{"value": nil}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool := &schema.ToolInfo{Name: "invalid", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"outer": tc.parameter})}
			if _, err := projectToolDefinition(tool, DefaultProjectionLimits()); err == nil {
				t.Fatal("invalid parameter tree was accepted")
			}
		})
	}

	valid := &schema.ToolInfo{Name: "valid", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"anything": {Type: schema.Array},
		"objects": {
			Type: schema.Array,
			ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
				"label": {Type: schema.String, Enum: []string{"one", "two"}},
			}},
		},
	})}
	projected, err := projectToolDefinition(valid, DefaultProjectionLimits())
	if err != nil {
		t.Fatalf("valid nested parameter tree rejected: %v", err)
	}
	projected.Params["objects"].ElemInfo.Type = schema.DataType("decimal")
	if err := validatePublicToolDefinition(&projected, DefaultProjectionLimits()); err == nil {
		t.Fatal("caller-mutated invalid public parameter tree was accepted")
	}
}

func TestToolDefinitionAppliesLimitsBeforeClone(t *testing.T) {
	t.Parallel()
	chain := func(depth int) *schema.ParameterInfo {
		root := &schema.ParameterInfo{Type: schema.String}
		current := root
		for i := 1; i < depth; i++ {
			root = &schema.ParameterInfo{Type: schema.Array, ElemInfo: current}
			current = root
		}
		return current
	}
	tool := func(parameter *schema.ParameterInfo) *schema.ToolInfo {
		return &schema.ToolInfo{Name: "bounded", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"value": parameter})}
	}

	limits := DefaultProjectionLimits()
	limits.MaxJSONDepth = 2
	if _, err := projectToolDefinition(tool(chain(2)), limits); err != nil {
		t.Fatalf("exact depth rejected: %v", err)
	}
	if _, err := projectToolDefinition(tool(chain(3)), limits); err == nil {
		t.Fatal("one-over depth was accepted")
	}

	limits = DefaultProjectionLimits()
	limits.MaxJSONEntries = 3
	parameter := &schema.ParameterInfo{Type: schema.String, Enum: []string{"one", "two"}}
	if _, err := projectToolDefinition(tool(parameter), limits); err != nil {
		t.Fatalf("exact entry count rejected: %v", err)
	}
	parameter.Enum = append(parameter.Enum, "three")
	if _, err := projectToolDefinition(tool(parameter), limits); err == nil {
		t.Fatal("one-over entry count was accepted")
	}

	jsonTool := func(value *jsonschema.Schema) *schema.ToolInfo {
		return &schema.ToolInfo{Name: "json-bounded", ParamsOneOf: schema.NewParamsOneOfByJSONSchema(value)}
	}
	simpleSchema := &jsonschema.Schema{Type: "string"}
	nestedSchema := &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}
	limits = DefaultProjectionLimits()
	limits.MaxJSONDepth = 2
	if _, err := projectToolDefinition(jsonTool(simpleSchema), limits); err != nil {
		t.Fatalf("exact JSON schema depth rejected: %v", err)
	}
	if _, err := projectToolDefinition(jsonTool(nestedSchema), limits); err == nil {
		t.Fatal("one-over JSON schema depth was accepted")
	}

	limits = DefaultProjectionLimits()
	limits.MaxJSONEntries = 4
	enumSchema := &jsonschema.Schema{Type: "string", Enum: []any{"one", "two"}}
	if _, err := projectToolDefinition(jsonTool(enumSchema), limits); err != nil {
		t.Fatalf("exact JSON schema entry count rejected: %v", err)
	}
	limits.MaxJSONEntries--
	if _, err := projectToolDefinition(jsonTool(enumSchema), limits); err == nil {
		t.Fatal("one-over JSON schema entry count was accepted")
	}

	projected, err := projectToolDefinition(jsonTool(simpleSchema), DefaultProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	limits = DefaultProjectionLimits()
	limits.MaxBlockBytes = len(projected.JSONSchema)
	if _, err := projectToolDefinition(jsonTool(simpleSchema), limits); err != nil {
		t.Fatalf("exact JSON schema byte limit rejected: %v", err)
	}
	limits.MaxBlockBytes--
	if _, err := projectToolDefinition(jsonTool(simpleSchema), limits); err == nil {
		t.Fatal("one-over JSON schema byte limit was accepted")
	}
}

func TestToolDefinitionCollectionsRejectNilEmptyAndDuplicateEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		block *schema.ContentBlock
	}{
		{name: "tool search nil", block: schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{nil}}})},
		{name: "tool search empty", block: schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{{}}}})},
		{name: "tool search duplicate", block: schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{{Name: "same"}, {Name: "same"}}}})},
		{name: "MCP list nil", block: schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{nil}})},
		{name: "MCP list empty", block: schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{{}}})},
		{name: "MCP list duplicate", block: schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{{Name: "same"}, {Name: "same"}}})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			role := schema.AgenticRoleTypeAssistant
			if tc.block.Type == schema.ContentBlockTypeToolSearchResult {
				role = schema.AgenticRoleTypeUser
			}
			if _, err := ProjectAgenticMessage(&schema.AgenticMessage{Role: role, ContentBlocks: []*schema.ContentBlock{tc.block}}, testContext(AgenticBlockContext{BlockID: "block"})); err == nil {
				t.Fatal("malformed tool collection was accepted")
			}
		})
	}
}

func TestMCPListToolSchemasApplyLimits(t *testing.T) {
	t.Parallel()
	tool := func(input *jsonschema.Schema) *schema.AgenticMessage {
		return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{{Name: "lookup", InputSchema: input}}}),
		}}
	}
	simple := &jsonschema.Schema{Type: "string"}
	nested := &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}
	ctx := testContext(AgenticBlockContext{BlockID: "block"})
	ctx.Limits.MaxJSONDepth = 2
	if _, err := ProjectAgenticMessage(tool(simple), ctx); err != nil {
		t.Fatalf("exact MCP schema depth rejected: %v", err)
	}
	if _, err := ProjectAgenticMessage(tool(nested), ctx); err == nil {
		t.Fatal("one-over MCP schema depth was accepted")
	}

	ctx = testContext(AgenticBlockContext{BlockID: "block"})
	ctx.Limits.MaxJSONEntries = 4
	enumSchema := &jsonschema.Schema{Type: "string", Enum: []any{"one", "two"}}
	if _, err := ProjectAgenticMessage(tool(enumSchema), ctx); err != nil {
		t.Fatalf("exact MCP schema entry count rejected: %v", err)
	}
	ctx.Limits.MaxJSONEntries--
	if _, err := ProjectAgenticMessage(tool(enumSchema), ctx); err == nil {
		t.Fatal("one-over MCP schema entry count was accepted")
	}
}

func TestPublicToolSchemasRevalidateBeforeDigest(t *testing.T) {
	t.Parallel()
	tooDeep := json.RawMessage(strings.Repeat(`{"nested":`, DefaultProjectionLimits().MaxJSONDepth) + `null` + strings.Repeat(`}`, DefaultProjectionLimits().MaxJSONDepth))

	mcpMessage := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{
			{Name: "first", InputSchema: &jsonschema.Schema{Type: "string"}},
			{Name: "second"},
		}}),
	}}
	projectMCP := func(t *testing.T) *PublicAgenticMessage {
		t.Helper()
		projection, err := ProjectAgenticMessage(mcpMessage, testContext(AgenticBlockContext{BlockID: "mcp-list"}))
		if err != nil {
			t.Fatal(err)
		}
		return projection
	}
	t.Run("duplicate MCP name", func(t *testing.T) {
		projection := projectMCP(t)
		projection.ContentBlocks[0].MCPListToolsResult.Tools[1].Name = "first"
		if _, err := ProjectionDigestV1(projection); err == nil {
			t.Fatal("duplicate caller-mutated MCP tool name was accepted")
		}
	})
	t.Run("deep MCP schema", func(t *testing.T) {
		projection := projectMCP(t)
		projection.ContentBlocks[0].MCPListToolsResult.Tools[0].InputSchema = tooDeep
		if _, err := ProjectionDigestV1(projection); err == nil {
			t.Fatal("deep caller-mutated MCP schema was accepted")
		}
	})
	t.Run("scalar MCP schema", func(t *testing.T) {
		projection := projectMCP(t)
		projection.ContentBlocks[0].MCPListToolsResult.Tools[0].InputSchema = json.RawMessage(`true`)
		if _, err := ProjectionDigestV1(projection); err == nil {
			t.Fatal("scalar caller-mutated MCP schema was accepted")
		}
	})

	searchMessage := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{
			{Name: "lookup", ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&jsonschema.Schema{Type: "string"})},
		}}}),
	}}
	projection, err := ProjectAgenticMessage(searchMessage, testContext(AgenticBlockContext{BlockID: "search"}))
	if err != nil {
		t.Fatal(err)
	}
	projection.ContentBlocks[0].ToolSearchResult.Tools[0].JSONSchema = tooDeep
	if _, err := ProjectionDigestV1(projection); err == nil {
		t.Fatal("deep caller-mutated tool-search schema was accepted")
	}
	projection, err = ProjectAgenticMessage(searchMessage, testContext(AgenticBlockContext{BlockID: "search"}))
	if err != nil {
		t.Fatal(err)
	}
	projection.ContentBlocks[0].ToolSearchResult.Tools[0].JSONSchema = json.RawMessage(`null`)
	if _, err := ProjectionDigestV1(projection); err == nil {
		t.Fatal("null caller-mutated tool-search schema was accepted")
	}
}

func TestAnnotationLimitIsSharedAcrossProviders(t *testing.T) {
	t.Parallel()
	limits := DefaultProjectionLimits()
	limits.MaxAnnotations = 1
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{
		Text: "source",
		OpenAIExtension: &openai.AssistantGenTextExtension{Annotations: []*openai.TextAnnotation{{
			Type:         openai.TextAnnotationTypeFileCitation,
			FileCitation: &openai.TextAnnotationFileCitation{FileID: "file", Index: 0},
		}}},
		ClaudeExtension: &claude.AssistantGenTextExtension{Citations: []*claude.TextCitation{{
			Type:                    claude.TextCitationTypeWebSearchResultLocation,
			WebSearchResultLocation: &claude.CitationWebSearchResultLocation{Title: "source", URL: "https://example.test"},
		}}},
	})}}
	ctx := testContext(AgenticBlockContext{BlockID: "text"})
	ctx.Limits = limits
	if _, err := ProjectAgenticMessage(message, ctx); err == nil {
		t.Fatal("combined one-over annotation count was accepted")
	}
}

func TestBoundedCounterRejectsIntegerOverflow(t *testing.T) {
	t.Parallel()
	total := math.MaxInt - 1
	if err := addBoundedCount(&total, 2, math.MaxInt, "overflow"); err == nil {
		t.Fatal("overflowing count was accepted")
	}
	if total != math.MaxInt-1 {
		t.Fatalf("rejected count changed total to %d", total)
	}
}

func TestProviderCitationVariantsAndRanges(t *testing.T) {
	t.Parallel()
	const text = "café source"
	annotated := schema.NewContentBlock(&schema.AssistantGenText{
		Text: text,
		OpenAIExtension: &openai.AssistantGenTextExtension{Annotations: []*openai.TextAnnotation{
			{Type: openai.TextAnnotationTypeFileCitation, FileCitation: &openai.TextAnnotationFileCitation{FileID: "file", Filename: "source.txt", Index: 1}},
			{Type: openai.TextAnnotationTypeURLCitation, URLCitation: &openai.TextAnnotationURLCitation{Title: "source", URL: "https://example.test", StartIndex: 6, EndIndex: 12}},
			{Type: openai.TextAnnotationTypeContainerFileCitation, ContainerFileCitation: &openai.TextAnnotationContainerFileCitation{ContainerID: "container", FileID: "file", Filename: "source.txt", StartIndex: 6, EndIndex: 12}},
			{Type: openai.TextAnnotationTypeFilePath, FilePath: &openai.TextAnnotationFilePath{FileID: "file", Index: 2}},
		}},
		ClaudeExtension: &claude.AssistantGenTextExtension{Citations: []*claude.TextCitation{
			{Type: claude.TextCitationTypeCharLocation, CharLocation: &claude.CitationCharLocation{CitedText: "chars", DocumentTitle: "doc", DocumentIndex: 0, StartCharIndex: 1, EndCharIndex: 2}},
			{Type: claude.TextCitationTypePageLocation, PageLocation: &claude.CitationPageLocation{CitedText: "pages", DocumentTitle: "doc", DocumentIndex: 0, StartPageNumber: 3, EndPageNumber: 4}},
			{Type: claude.TextCitationTypeContentBlockLocation, ContentBlockLocation: &claude.CitationContentBlockLocation{CitedText: "block", DocumentTitle: "doc", DocumentIndex: 0, StartBlockIndex: 5, EndBlockIndex: 6}},
			{Type: claude.TextCitationTypeWebSearchResultLocation, WebSearchResultLocation: &claude.CitationWebSearchResultLocation{CitedText: "web", Title: "source", URL: "https://example.test"}},
		}},
	})
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.AssistantGenText{Text: "unrelated"}),
		annotated,
	}}
	projected, err := ProjectAgenticMessage(message, testContext(AgenticBlockContext{BlockID: "plain"}, AgenticBlockContext{BlockID: "annotated"}))
	if err != nil {
		t.Fatal(err)
	}
	annotations := projected.ContentBlocks[1].ProviderAnnotations
	if annotations == nil || len(annotations.OpenAI) != 4 || len(annotations.Claude) != 4 {
		t.Fatalf("annotations = %#v", annotations)
	}
	urlCitation := annotations.OpenAI[1]
	if got := (*projected.ContentBlocks[1].Text)[urlCitation.StartIndex:urlCitation.EndIndex]; got != "source" {
		t.Fatalf("reconstructed citation span = %q", got)
	}
	if projected.ContentBlocks[0].ProviderAnnotations != nil {
		t.Fatal("annotations were associated with the wrong assistant text block")
	}

	invalid := []struct {
		name       string
		annotation *openai.TextAnnotation
		citation   *claude.TextCitation
	}{
		{name: "OpenAI negative", annotation: &openai.TextAnnotation{Type: openai.TextAnnotationTypeURLCitation, URLCitation: &openai.TextAnnotationURLCitation{URL: "https://example.test", StartIndex: -1, EndIndex: 1}}},
		{name: "OpenAI reversed", annotation: &openai.TextAnnotation{Type: openai.TextAnnotationTypeURLCitation, URLCitation: &openai.TextAnnotationURLCitation{URL: "https://example.test", StartIndex: 6, EndIndex: 5}}},
		{name: "OpenAI out of range", annotation: &openai.TextAnnotation{Type: openai.TextAnnotationTypeURLCitation, URLCitation: &openai.TextAnnotationURLCitation{URL: "https://example.test", StartIndex: 6, EndIndex: 99}}},
		{name: "OpenAI mid rune", annotation: &openai.TextAnnotation{Type: openai.TextAnnotationTypeURLCitation, URLCitation: &openai.TextAnnotationURLCitation{URL: "https://example.test", StartIndex: 4, EndIndex: 5}}},
		{name: "OpenAI ambiguous", annotation: &openai.TextAnnotation{Type: openai.TextAnnotationTypeURLCitation, URLCitation: &openai.TextAnnotationURLCitation{URL: "https://example.test", StartIndex: 0, EndIndex: 1}, FileCitation: &openai.TextAnnotationFileCitation{FileID: "file"}}},
		{name: "Claude negative", citation: &claude.TextCitation{Type: claude.TextCitationTypeCharLocation, CharLocation: &claude.CitationCharLocation{DocumentIndex: -1, StartCharIndex: 0, EndCharIndex: 1}}},
		{name: "Claude reversed", citation: &claude.TextCitation{Type: claude.TextCitationTypePageLocation, PageLocation: &claude.CitationPageLocation{StartPageNumber: 2, EndPageNumber: 1}}},
		{name: "Claude ambiguous", citation: &claude.TextCitation{Type: claude.TextCitationTypeCharLocation, CharLocation: &claude.CitationCharLocation{StartCharIndex: 0, EndCharIndex: 1}, PageLocation: &claude.CitationPageLocation{StartPageNumber: 0, EndPageNumber: 1}}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			value := &schema.AssistantGenText{Text: text}
			if tc.annotation != nil {
				value.OpenAIExtension = &openai.AssistantGenTextExtension{Annotations: []*openai.TextAnnotation{tc.annotation}}
			}
			if tc.citation != nil {
				value.ClaudeExtension = &claude.AssistantGenTextExtension{Citations: []*claude.TextCitation{tc.citation}}
			}
			if _, err := ProjectAgenticMessage(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(value)}}, testContext(AgenticBlockContext{BlockID: "text"})); err == nil {
				t.Fatal("invalid citation was accepted")
			}
		})
	}
}

func TestGeminiGroundingTargetsExactAssistantTextBlock(t *testing.T) {
	t.Parallel()
	message := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "first"}),
			schema.NewContentBlock(&schema.AssistantGenImage{URL: "https://example.test/image"}),
			schema.NewContentBlock(&schema.AssistantGenText{Text: "café source"}),
		},
		ResponseMeta: &schema.AgenticResponseMeta{GeminiExtension: &gemini.ResponseMetaExtension{GroundingMeta: &gemini.GroundingMetadata{
			GroundingChunks: []*gemini.GroundingChunk{{Web: &gemini.GroundingChunkWeb{Title: "reference", URI: "https://example.test/source"}}},
			GroundingSupports: []*gemini.GroundingSupport{{
				ConfidenceScores:      []float32{0.9},
				GroundingChunkIndices: []int{0},
				Segment:               &gemini.Segment{PartIndex: 2, StartIndex: 6, EndIndex: 12, Text: "source"},
			}},
		}}},
	}
	contexts := []AgenticBlockContext{{BlockID: "first"}, {BlockID: "image"}, {BlockID: "second"}}
	projected, err := ProjectAgenticMessage(message, testContext(contexts...))
	if err != nil {
		t.Fatal(err)
	}
	support := projected.ResponseMeta.GeminiGrounding.Supports[0]
	if support.PartIndex != 2 || support.Text != (*projected.ContentBlocks[2].Text)[support.StartIndex:support.EndIndex] {
		t.Fatalf("support = %#v", support)
	}

	tests := []struct {
		name   string
		mutate func(*gemini.Segment)
	}{
		{"wrong text", func(segment *gemini.Segment) { segment.Text = "wrong!" }},
		{"non-text target", func(segment *gemini.Segment) { segment.PartIndex = 1 }},
		{"mid-rune offset", func(segment *gemini.Segment) { segment.StartIndex = 4; segment.EndIndex = 6; segment.Text = "" }},
		{"out of range", func(segment *gemini.Segment) { segment.EndIndex = 99 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copyMessage := *message
			copyMeta := *message.ResponseMeta
			copyExtension := *message.ResponseMeta.GeminiExtension
			copyGrounding := *message.ResponseMeta.GeminiExtension.GroundingMeta
			copySupport := *copyGrounding.GroundingSupports[0]
			copySegment := *copySupport.Segment
			tc.mutate(&copySegment)
			copySupport.Segment = &copySegment
			copyGrounding.GroundingSupports = []*gemini.GroundingSupport{&copySupport}
			copyExtension.GroundingMeta = &copyGrounding
			copyMeta.GeminiExtension = &copyExtension
			copyMessage.ResponseMeta = &copyMeta
			if _, err := ProjectAgenticMessage(&copyMessage, testContext(contexts...)); err == nil {
				t.Fatal("invalid grounding was accepted")
			}
		})
	}
}

func TestProjectionDigestRejectsMutatedPublicProjection(t *testing.T) {
	t.Parallel()
	newProjection := func(t *testing.T) *PublicAgenticMessage {
		t.Helper()
		message := &schema.AgenticMessage{
			Role:          schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "source"})},
			ResponseMeta: &schema.AgenticResponseMeta{GeminiExtension: &gemini.ResponseMetaExtension{GroundingMeta: &gemini.GroundingMetadata{
				GroundingChunks: []*gemini.GroundingChunk{{Web: &gemini.GroundingChunkWeb{URI: "https://example.test"}}},
				GroundingSupports: []*gemini.GroundingSupport{{
					GroundingChunkIndices: []int{0}, Segment: &gemini.Segment{PartIndex: 0, StartIndex: 0, EndIndex: 6, Text: "source"},
				}},
			}}},
		}
		projection, err := ProjectAgenticMessage(message, testContext(AgenticBlockContext{BlockID: "block"}))
		if err != nil {
			t.Fatal(err)
		}
		return projection
	}
	for _, tc := range []struct {
		name   string
		mutate func(*PublicAgenticMessage)
	}{
		{"role", func(projection *PublicAgenticMessage) { projection.Role = "invalid" }},
		{"base block scope", func(projection *PublicAgenticMessage) { projection.Identity.BlockID = "block" }},
		{"block identity", func(projection *PublicAgenticMessage) { projection.ContentBlocks[0].Identity.MessageID = "different" }},
		{"grounding target", func(projection *PublicAgenticMessage) {
			projection.ResponseMeta.GeminiGrounding.Supports[0].Text = "altered"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projection := newProjection(t)
			tc.mutate(projection)
			if _, err := ProjectionDigestV1(projection); err == nil {
				t.Fatal("mutated projection was accepted")
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

	envelope := &AgenticEnvelopeV1{Version: 1, Kind: EnvelopePaused, Identity: testIdentity(), Paused: &PausedV1{PauseID: "pause",
		Targets:     []InterruptTargetV1{{ID: "interrupt", Address: "node/0"}},
		Correlation: &ApprovalInterruptCorrelation{ApprovalRequestID: "approval", InterruptTargetID: "different", InterruptAddress: "node/0"},
	}}
	if _, err := LifecycleDigestV1(envelope); err == nil {
		t.Fatal("unvalidated approval correlation was accepted")
	}
}

func TestProjectionAcceptsExactAndRejectsOneOverEncodedByteLimits(t *testing.T) {
	t.Parallel()
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.AssistantGenText{Text: "bounded"}),
	}}
	baseContext := testContext(AgenticBlockContext{BlockID: "block"})
	projected, err := ProjectAgenticMessage(message, baseContext)
	if err != nil {
		t.Fatal(err)
	}
	blockJSON, err := json.Marshal(projected.ContentBlocks[0])
	if err != nil {
		t.Fatal(err)
	}
	messageWithoutDigest := *projected
	messageWithoutDigest.Digest = ""
	messageJSON, err := json.Marshal(&messageWithoutDigest)
	if err != nil {
		t.Fatal(err)
	}

	exactBlock := baseContext
	exactBlock.Limits.MaxBlockBytes = len(blockJSON)
	if _, err := ProjectAgenticMessage(message, exactBlock); err != nil {
		t.Fatalf("exact block byte limit rejected: %v", err)
	}
	overBlock := exactBlock
	overBlock.Limits.MaxBlockBytes--
	if _, err := ProjectAgenticMessage(message, overBlock); err == nil {
		t.Fatal("one-over block byte limit was accepted")
	}

	exactMessage := baseContext
	exactMessage.Limits.MaxMessageBytes = len(messageJSON)
	if _, err := ProjectAgenticMessage(message, exactMessage); err != nil {
		t.Fatalf("exact message byte limit rejected: %v", err)
	}
	overMessage := exactMessage
	overMessage.Limits.MaxMessageBytes--
	if _, err := ProjectAgenticMessage(message, overMessage); err == nil {
		t.Fatal("one-over message byte limit was accepted")
	}
}

func TestEnvelopeStrictDecodeRejectsMalformedProviderAnnotation(t *testing.T) {
	t.Parallel()
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.AssistantGenText{Text: "source", OpenAIExtension: &openai.AssistantGenTextExtension{Annotations: []*openai.TextAnnotation{{
			Type: openai.TextAnnotationTypeURLCitation,
			URLCitation: &openai.TextAnnotationURLCitation{
				URL: "https://example.test", StartIndex: 0, EndIndex: 6,
			},
		}}}}),
	}}
	projection, err := ToAgenticProjection(message, testContext(AgenticBlockContext{BlockID: "block"}))
	if err != nil {
		t.Fatal(err)
	}
	envelope := projection.Blocks[0].Supplement
	envelope.ContentBlock.ProviderAnnotations.OpenAI[0].URL = ""
	encoded, err := json.Marshal(&envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAgenticEnvelope(encoded); err == nil {
		t.Fatal("malformed provider annotation was accepted")
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
	independentDigest := func(prefix, canonical string) CandidateDigestV1 {
		sum := sha256.Sum256(append([]byte(prefix), canonical...))
		return CandidateDigestV1(fmt.Sprintf("%x", sum))
	}
	manualProjection := `{"contentBlocks":[],"identity":{"agentPath":[{"name":"root","runId":"run-1"}],"attemptId":"attempt-1","messageId":"message-1","runId":"run-1","sessionId":"session-1","threadId":"session-1","turnId":"turn-1"},"role":"assistant","version":1}`
	manualLifecycle := `{"identity":{"agentPath":[{"name":"root","runId":"run-1"}],"attemptId":"attempt-1","messageId":"message-1","runId":"run-1","sessionId":"session-1","threadId":"session-1","turnId":"turn-1"},"kind":"turn_finished","lifecycle":{"detail":"committed"},"version":1}`
	if got := independentDigest("eino-agentic-v1\x00projection\x00", manualProjection); got != wantProjection {
		t.Fatalf("independent projection digest = %s", got)
	}
	if got := independentDigest("eino-agentic-v1\x00lifecycle\x00turn_finished\x00", manualLifecycle); got != wantLifecycle {
		t.Fatalf("independent lifecycle digest = %s", got)
	}
	if projectionDigest != wantProjection {
		t.Fatalf("projection digest = %s", projectionDigest)
	}
	if lifecycleDigest != wantLifecycle {
		t.Fatalf("lifecycle digest = %s", lifecycleDigest)
	}
}

func TestProjectionDigestMatchesIndependentCanonicalMapNumericAndStringVector(t *testing.T) {
	t.Parallel()
	project := func(arguments map[string]any) *PublicAgenticMessage {
		t.Helper()
		projection, err := ToAgenticProjection(
			&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.ServerToolCall{CallID: "server-edge", Name: "lookup", Arguments: arguments}),
			}},
			testContext(AgenticBlockContext{BlockID: "block-edge", ProviderServerID: "provider-edge"}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return projection.Public
	}

	first := project(map[string]any{
		"z":      json.Number("1.0"),
		"a":      "é/雪",
		"nested": []any{true, nil, json.Number("-0")},
	})
	second := project(map[string]any{
		"nested": []any{true, nil, float64(0)},
		"a":      "é/雪",
		"z":      float64(1),
	})
	if first.Digest != second.Digest {
		t.Fatalf("equivalent map/numeric projections differ: %s != %s", first.Digest, second.Digest)
	}

	manualCanonical := `{"contentBlocks":[{"identity":{"agentPath":[{"name":"root","runId":"run-1"}],"attemptId":"attempt-1","blockId":"block-edge","callId":"server-edge","messageId":"message-1","runId":"run-1","sessionId":"session-1","threadId":"session-1","turnId":"turn-1"},"serverToolCall":{"arguments":{"a":"é/雪","nested":[true,null,0],"z":1},"callId":"server-edge","executionOwner":"provider","name":"lookup","providerServerId":"provider-edge"},"type":"server_tool_call"}],"identity":{"agentPath":[{"name":"root","runId":"run-1"}],"attemptId":"attempt-1","messageId":"message-1","runId":"run-1","sessionId":"session-1","threadId":"session-1","turnId":"turn-1"},"role":"assistant","version":1}`
	sum := sha256.Sum256(append([]byte("eino-agentic-v1\x00projection\x00"), manualCanonical...))
	want := CandidateDigestV1(fmt.Sprintf("%x", sum))
	if first.Digest != want {
		t.Fatalf("projection digest = %s, independent canonical digest = %s", first.Digest, want)
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

func FuzzAgenticUnionValidation(f *testing.F) {
	f.Add(uint8(1), uint8(0), uint8(1), uint8(0))
	f.Add(uint8(3), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(8), uint8(3), uint8(3), uint8(0))
	f.Add(uint8(8), uint8(3), uint8(16), uint8(4))
	f.Fuzz(func(t *testing.T, outerMask, outerDiscriminator, nestedMask, nestedDiscriminator uint8) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("panic: %v", recovered)
			}
		}()

		nestedKinds := []schema.FunctionToolResultContentBlockType{
			schema.FunctionToolResultContentBlockTypeText,
			schema.FunctionToolResultContentBlockTypeImage,
			schema.FunctionToolResultContentBlockTypeAudio,
			schema.FunctionToolResultContentBlockTypeVideo,
			schema.FunctionToolResultContentBlockTypeFile,
		}
		nested := &schema.FunctionToolResultContentBlock{Type: nestedKinds[int(nestedDiscriminator)%len(nestedKinds)]}
		if nestedMask&1 != 0 {
			nested.Text = &schema.UserInputText{Text: "text"}
		}
		if nestedMask&2 != 0 {
			nested.Image = &schema.UserInputImage{URL: "https://example.test/image"}
		}
		if nestedMask&4 != 0 {
			nested.Audio = &schema.UserInputAudio{URL: "https://example.test/audio"}
		}
		if nestedMask&8 != 0 {
			nested.Video = &schema.UserInputVideo{URL: "https://example.test/video"}
		}
		if nestedMask&16 != 0 {
			nested.File = &schema.UserInputFile{URL: "https://example.test/file", Name: "file"}
		}

		outerKinds := []schema.ContentBlockType{
			schema.ContentBlockTypeReasoning,
			schema.ContentBlockTypeAssistantGenText,
			schema.ContentBlockTypeFunctionToolCall,
			schema.ContentBlockTypeFunctionToolResult,
		}
		block := &schema.ContentBlock{Type: outerKinds[int(outerDiscriminator)%len(outerKinds)]}
		if outerMask&1 != 0 {
			block.Reasoning = &schema.Reasoning{Text: "reason"}
		}
		if outerMask&2 != 0 {
			block.AssistantGenText = &schema.AssistantGenText{Text: "answer"}
		}
		if outerMask&4 != 0 {
			block.FunctionToolCall = &schema.FunctionToolCall{CallID: "call", Name: "lookup", Arguments: `{}`}
		}
		if outerMask&8 != 0 {
			block.FunctionToolResult = &schema.FunctionToolResult{CallID: "call", Name: "lookup", Content: []*schema.FunctionToolResultContentBlock{nested}}
		}
		role := schema.AgenticRoleTypeAssistant
		if block.Type == schema.ContentBlockTypeFunctionToolResult {
			role = schema.AgenticRoleTypeUser
		}
		project := func() ([]byte, string) {
			value, err := ProjectAgenticMessage(&schema.AgenticMessage{Role: role, ContentBlocks: []*schema.ContentBlock{block}}, testContext(AgenticBlockContext{BlockID: "block"}))
			if err != nil {
				return nil, err.Error()
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err.Error()
			}
			return encoded, ""
		}
		firstValue, firstError := project()
		secondValue, secondError := project()
		if firstError != secondError || !bytes.Equal(firstValue, secondValue) {
			t.Fatalf("nondeterministic union result: first=(%q,%s) second=(%q,%s)", firstValue, firstError, secondValue, secondError)
		}
	})
}
