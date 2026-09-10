package convert

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino/schema/claude"
	"github.com/cloudwego/eino/schema/gemini"
	"github.com/cloudwego/eino/schema/openai"
	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

type ProjectionError struct {
	Block int
	Path  string
	Err   error
}

func (e *ProjectionError) Error() string {
	if e.Block >= 0 {
		return fmt.Sprintf("agentic projection block %d field %s: %v", e.Block, e.Path, e.Err)
	}
	return fmt.Sprintf("agentic projection field %s: %v", e.Path, e.Err)
}

func (e *ProjectionError) Unwrap() error { return e.Err }

func projectErr(block int, path, message string) error {
	return &ProjectionError{Block: block, Path: path, Err: errors.New(message)}
}

func validateLimits(l ProjectionLimits) error {
	v := reflect.ValueOf(l)
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Int() <= 0 {
			return fmt.Errorf("%s must be positive", t.Field(i).Name)
		}
	}
	return nil
}

func validateIdentity(id AgenticIdentityV1, maxPath int, requireBlock bool) error {
	if id.SessionID == "" || id.RunID == "" || id.TurnID == "" || id.MessageID == "" || id.AttemptID == "" {
		return errors.New("session, run, turn, message, and attempt IDs are required")
	}
	if id.ThreadID != id.SessionID {
		return errors.New("thread ID must equal session ID")
	}
	if requireBlock && id.BlockID == "" {
		return errors.New("block ID is required")
	}
	if len(id.AgentPath) == 0 || len(id.AgentPath) > maxPath {
		return errors.New("agent path must be non-empty and within the configured limit")
	}
	for _, segment := range id.AgentPath {
		if segment.Name == "" || segment.RunID == "" {
			return errors.New("agent path names and run IDs are required")
		}
	}
	return nil
}

func ProjectAgenticMessage(msg *schema.AgenticMessage, ctx AgenticProjectionContext) (*PublicAgenticMessage, error) {
	if msg == nil {
		return nil, projectErr(-1, "message", "must not be nil")
	}
	if err := validateLimits(ctx.Limits); err != nil {
		return nil, projectErr(-1, "limits", err.Error())
	}
	if err := validateIdentity(ctx.Identity, ctx.Limits.MaxAgentPathSegments, false); err != nil {
		return nil, projectErr(-1, "identity", err.Error())
	}
	if msg.Role != schema.AgenticRoleTypeSystem && msg.Role != schema.AgenticRoleTypeUser && msg.Role != schema.AgenticRoleTypeAssistant {
		return nil, projectErr(-1, "role", "unknown role")
	}
	if len(msg.ContentBlocks) > ctx.Limits.MaxBlocks {
		return nil, projectErr(-1, "contentBlocks", "block limit exceeded")
	}
	if len(ctx.Blocks) != len(msg.ContentBlocks) {
		return nil, projectErr(-1, "context.blocks", "must contain one entry per content block")
	}

	out := &PublicAgenticMessage{Version: AgenticSchemaVersion, Identity: cloneIdentity(ctx.Identity), Role: msg.Role}
	out.ContentBlocks = make([]PublicContentBlock, len(msg.ContentBlocks))
	seenBlocks := make(map[string]struct{}, len(msg.ContentBlocks))
	seenOwnedIDs := make(map[string]struct{}, len(msg.ContentBlocks))
	for i, block := range msg.ContentBlocks {
		bc := ctx.Blocks[i]
		if bc.BlockID == "" {
			return nil, projectErr(i, "context.blockId", "is required")
		}
		if _, duplicate := seenBlocks[bc.BlockID]; duplicate {
			return nil, projectErr(i, "context.blockId", "must be unique")
		}
		seenBlocks[bc.BlockID] = struct{}{}
		projected, err := projectBlock(block, bc, ctx.Identity, msg.Role, ctx.Limits, i)
		if err != nil {
			return nil, err
		}
		if err := validateOwnedIDUnique(&projected, seenOwnedIDs); err != nil {
			return nil, projectErr(i, "identity", err.Error())
		}
		encoded, err := json.Marshal(projected)
		if err != nil || len(encoded) > ctx.Limits.MaxBlockBytes {
			return nil, projectErr(i, "block", "encoded block limit exceeded")
		}
		out.ContentBlocks[i] = projected
	}
	meta, err := projectResponseMeta(msg.ResponseMeta, out.ContentBlocks, ctx.Limits)
	if err != nil {
		return nil, err
	}
	out.ResponseMeta = meta
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, projectErr(-1, "message", "cannot encode public projection")
	}
	if len(encoded) > ctx.Limits.MaxMessageBytes {
		return nil, projectErr(-1, "message", "encoded message limit exceeded")
	}
	digest, err := ProjectionDigestV1(out)
	if err != nil {
		return nil, err
	}
	out.Digest = digest
	return out, nil
}

func projectBlock(block *schema.ContentBlock, bc AgenticBlockContext, base AgenticIdentityV1, role schema.AgenticRoleType, limits ProjectionLimits, ordinal int) (PublicContentBlock, error) {
	if block == nil {
		return PublicContentBlock{}, projectErr(ordinal, "block", "must not be nil")
	}
	selected, count := selectedBlockVariant(block)
	if count != 1 || selected != block.Type {
		return PublicContentBlock{}, projectErr(ordinal, "union", "discriminator must select exactly one matching payload")
	}
	if !roleAllowsBlock(role, block.Type) {
		return PublicContentBlock{}, projectErr(ordinal, "type", "is incompatible with message role")
	}
	id := cloneIdentity(base)
	id.BlockID = bc.BlockID
	out := PublicContentBlock{Type: block.Type, Identity: id}
	var err error
	switch block.Type {
	case schema.ContentBlockTypeReasoning:
		out.Text = stringPtr(block.Reasoning.Text)
	case schema.ContentBlockTypeUserInputText:
		out.Text = stringPtr(block.UserInputText.Text)
	case schema.ContentBlockTypeUserInputImage:
		out.Media, err = projectMedia(block.UserInputImage.URL, block.UserInputImage.Base64Data, block.UserInputImage.MIMEType, "", block.UserInputImage.Detail)
	case schema.ContentBlockTypeUserInputAudio:
		out.Media, err = projectMedia(block.UserInputAudio.URL, block.UserInputAudio.Base64Data, block.UserInputAudio.MIMEType, "", "")
	case schema.ContentBlockTypeUserInputVideo:
		out.Media, err = projectMedia(block.UserInputVideo.URL, block.UserInputVideo.Base64Data, block.UserInputVideo.MIMEType, "", "")
	case schema.ContentBlockTypeUserInputFile:
		out.Media, err = projectMedia(block.UserInputFile.URL, block.UserInputFile.Base64Data, block.UserInputFile.MIMEType, block.UserInputFile.Name, "")
	case schema.ContentBlockTypeAssistantGenText:
		out.Text = stringPtr(block.AssistantGenText.Text)
		out.ProviderAnnotations, err = projectTextAnnotations(block.AssistantGenText, limits)
	case schema.ContentBlockTypeAssistantGenImage:
		out.Media, err = projectMedia(block.AssistantGenImage.URL, block.AssistantGenImage.Base64Data, block.AssistantGenImage.MIMEType, "", "")
	case schema.ContentBlockTypeAssistantGenAudio:
		out.Media, err = projectMedia(block.AssistantGenAudio.URL, block.AssistantGenAudio.Base64Data, block.AssistantGenAudio.MIMEType, "", "")
	case schema.ContentBlockTypeAssistantGenVideo:
		out.Media, err = projectMedia(block.AssistantGenVideo.URL, block.AssistantGenVideo.Base64Data, block.AssistantGenVideo.MIMEType, "", "")
	case schema.ContentBlockTypeToolSearchResult:
		out.ToolSearchResult, err = projectToolSearch(block.ToolSearchFunctionToolResult, limits)
		if out.ToolSearchResult != nil {
			out.Identity.CallID = out.ToolSearchResult.CallID
		}
	case schema.ContentBlockTypeFunctionToolCall:
		v := block.FunctionToolCall
		if v.CallID == "" || v.Name == "" {
			err = errors.New("call ID and name are required")
			break
		}
		out.Identity.CallID = v.CallID
		out.FunctionToolCall = &PublicFunctionToolCall{CallID: v.CallID, Name: v.Name, Arguments: v.Arguments}
	case schema.ContentBlockTypeFunctionToolResult:
		out.FunctionToolResult, err = projectFunctionResult(block.FunctionToolResult)
		if out.FunctionToolResult != nil {
			out.Identity.CallID = out.FunctionToolResult.CallID
		}
	case schema.ContentBlockTypeServerToolCall:
		if bc.ProviderServerID == "" {
			err = errors.New("provider server ID is required")
			break
		}
		v := block.ServerToolCall
		if v.CallID == "" || v.Name == "" {
			err = errors.New("server tool call ID and name are required")
			break
		}
		var args any
		args, err = clonePublicJSON(v.Arguments, limits)
		if err == nil {
			out.Identity.CallID = v.CallID
			out.ServerToolCall = &PublicServerToolCall{
				ProviderServerID: bc.ProviderServerID, CallID: v.CallID, Name: v.Name,
				Arguments: args, ExecutionOwner: ToolExecutionOwnerProvider,
			}
		}
	case schema.ContentBlockTypeServerToolResult:
		if bc.ProviderServerID == "" {
			err = errors.New("provider server ID is required")
			break
		}
		v := block.ServerToolResult
		if v.CallID == "" || v.Name == "" {
			err = errors.New("server tool result call ID and name are required")
			break
		}
		var content any
		content, err = clonePublicJSON(v.Content, limits)
		if err == nil {
			out.Identity.CallID = v.CallID
			out.ServerToolResult = &PublicServerToolResult{
				ProviderServerID: bc.ProviderServerID, CallID: v.CallID, Name: v.Name,
				Content: content, ExecutionOwner: ToolExecutionOwnerProvider,
			}
		}
	case schema.ContentBlockTypeMCPToolCall:
		v := block.MCPToolCall
		if v.ServerLabel == "" || v.CallID == "" || v.Name == "" {
			err = errors.New("MCP server label, call ID, and name are required")
			break
		}
		out.Identity.CallID = v.CallID
		out.MCPToolCall = &PublicMCPToolCall{
			ServerLabel: v.ServerLabel, ApprovalRequestID: v.ApprovalRequestID,
			CallID: v.CallID, Name: v.Name, Arguments: v.Arguments,
			ExecutionOwner: ToolExecutionOwnerProviderMCP,
		}
	case schema.ContentBlockTypeMCPToolResult:
		v := block.MCPToolResult
		if v.ServerLabel == "" || v.CallID == "" || v.Name == "" {
			err = errors.New("MCP server label, call ID, and name are required")
			break
		}
		out.Identity.CallID = v.CallID
		out.MCPToolResult = &PublicMCPToolResult{
			ServerLabel: v.ServerLabel, CallID: v.CallID, Name: v.Name, Content: v.Content,
			ExecutionOwner: ToolExecutionOwnerProviderMCP,
		}
		if v.Error != nil {
			code := v.Error.Code
			if code != nil {
				c := *code
				code = &c
			}
			out.MCPToolResult.Error = &PublicMCPError{Code: code, Message: v.Error.Message}
		}
	case schema.ContentBlockTypeMCPListToolsResult:
		out.MCPListToolsResult, err = projectMCPList(block.MCPListToolsResult, limits)
	case schema.ContentBlockTypeMCPToolApprovalRequest:
		v := block.MCPToolApprovalRequest
		if v.ID == "" || v.ServerLabel == "" || v.Name == "" {
			err = errors.New("approval ID, MCP server label, and name are required")
			break
		}
		out.MCPApprovalRequest = &PublicMCPApprovalRequest{v.ID, v.ServerLabel, v.Name, v.Arguments}
	case schema.ContentBlockTypeMCPToolApprovalResponse:
		v := block.MCPToolApprovalResponse
		if v.ApprovalRequestID == "" || bc.ExpectedApprovalRequestID == "" || v.ApprovalRequestID != bc.ExpectedApprovalRequestID {
			err = errors.New("approval request ID does not match caller context")
			break
		}
		out.MCPApprovalResponse = &PublicMCPApprovalResponse{v.ApprovalRequestID, v.Approve, v.Reason}
	default:
		err = errors.New("unknown content block type")
	}
	if err != nil {
		return PublicContentBlock{}, projectErr(ordinal, string(block.Type), err.Error())
	}
	return out, nil
}

func roleAllowsBlock(role schema.AgenticRoleType, kind schema.ContentBlockType) bool {
	switch role {
	case schema.AgenticRoleTypeSystem:
		return kind == schema.ContentBlockTypeUserInputText
	case schema.AgenticRoleTypeUser:
		switch kind {
		case schema.ContentBlockTypeUserInputText, schema.ContentBlockTypeUserInputImage,
			schema.ContentBlockTypeUserInputAudio, schema.ContentBlockTypeUserInputVideo,
			schema.ContentBlockTypeUserInputFile, schema.ContentBlockTypeToolSearchResult,
			schema.ContentBlockTypeFunctionToolResult, schema.ContentBlockTypeMCPToolApprovalResponse:
			return true
		}
	case schema.AgenticRoleTypeAssistant:
		switch kind {
		case schema.ContentBlockTypeReasoning, schema.ContentBlockTypeAssistantGenText,
			schema.ContentBlockTypeAssistantGenImage, schema.ContentBlockTypeAssistantGenAudio,
			schema.ContentBlockTypeAssistantGenVideo, schema.ContentBlockTypeFunctionToolCall,
			schema.ContentBlockTypeServerToolCall, schema.ContentBlockTypeServerToolResult,
			schema.ContentBlockTypeMCPToolCall, schema.ContentBlockTypeMCPToolResult,
			schema.ContentBlockTypeMCPListToolsResult, schema.ContentBlockTypeMCPToolApprovalRequest:
			return true
		}
	}
	return false
}

func selectedBlockVariant(b *schema.ContentBlock) (schema.ContentBlockType, int) {
	variants := []struct {
		kind schema.ContentBlockType
		set  bool
	}{
		{schema.ContentBlockTypeReasoning, b.Reasoning != nil}, {schema.ContentBlockTypeUserInputText, b.UserInputText != nil},
		{schema.ContentBlockTypeUserInputImage, b.UserInputImage != nil}, {schema.ContentBlockTypeUserInputAudio, b.UserInputAudio != nil},
		{schema.ContentBlockTypeUserInputVideo, b.UserInputVideo != nil}, {schema.ContentBlockTypeUserInputFile, b.UserInputFile != nil},
		{schema.ContentBlockTypeToolSearchResult, b.ToolSearchFunctionToolResult != nil}, {schema.ContentBlockTypeAssistantGenText, b.AssistantGenText != nil},
		{schema.ContentBlockTypeAssistantGenImage, b.AssistantGenImage != nil}, {schema.ContentBlockTypeAssistantGenAudio, b.AssistantGenAudio != nil},
		{schema.ContentBlockTypeAssistantGenVideo, b.AssistantGenVideo != nil}, {schema.ContentBlockTypeFunctionToolCall, b.FunctionToolCall != nil},
		{schema.ContentBlockTypeFunctionToolResult, b.FunctionToolResult != nil}, {schema.ContentBlockTypeServerToolCall, b.ServerToolCall != nil},
		{schema.ContentBlockTypeServerToolResult, b.ServerToolResult != nil}, {schema.ContentBlockTypeMCPToolCall, b.MCPToolCall != nil},
		{schema.ContentBlockTypeMCPToolResult, b.MCPToolResult != nil}, {schema.ContentBlockTypeMCPListToolsResult, b.MCPListToolsResult != nil},
		{schema.ContentBlockTypeMCPToolApprovalRequest, b.MCPToolApprovalRequest != nil}, {schema.ContentBlockTypeMCPToolApprovalResponse, b.MCPToolApprovalResponse != nil},
	}
	var selected schema.ContentBlockType
	count := 0
	for _, v := range variants {
		if v.set {
			selected = v.kind
			count++
		}
	}
	return selected, count
}

func projectMedia(url, data, mime, name string, detail schema.ImageURLDetail) (*PublicMedia, error) {
	if (url == "") == (data == "") {
		return nil, errors.New("exactly one URL or base64 source is required")
	}
	if data != "" && mime == "" {
		return nil, errors.New("base64 media requires a MIME type")
	}
	return &PublicMedia{URL: url, Base64Data: data, MIMEType: mime, Name: name, Detail: detail}, nil
}

func projectFunctionResult(v *schema.FunctionToolResult) (*PublicFunctionToolResult, error) {
	if v.CallID == "" || v.Name == "" {
		return nil, errors.New("call ID and name are required")
	}
	out := &PublicFunctionToolResult{CallID: v.CallID, Name: v.Name, Content: make([]PublicFunctionResultPart, len(v.Content))}
	for i, part := range v.Content {
		if part == nil {
			return nil, fmt.Errorf("content part %d is nil", i)
		}
		selected, count := selectedResultVariant(part)
		if count != 1 || selected != part.Type {
			return nil, fmt.Errorf("content part %d has invalid union", i)
		}
		p := PublicFunctionResultPart{Type: part.Type}
		var err error
		switch part.Type {
		case schema.FunctionToolResultContentBlockTypeText:
			p.Text = part.Text.Text
		case schema.FunctionToolResultContentBlockTypeImage:
			p.Media, err = projectMedia(part.Image.URL, part.Image.Base64Data, part.Image.MIMEType, "", part.Image.Detail)
		case schema.FunctionToolResultContentBlockTypeAudio:
			p.Media, err = projectMedia(part.Audio.URL, part.Audio.Base64Data, part.Audio.MIMEType, "", "")
		case schema.FunctionToolResultContentBlockTypeVideo:
			p.Media, err = projectMedia(part.Video.URL, part.Video.Base64Data, part.Video.MIMEType, "", "")
		case schema.FunctionToolResultContentBlockTypeFile:
			p.Media, err = projectMedia(part.File.URL, part.File.Base64Data, part.File.MIMEType, part.File.Name, "")
		default:
			err = errors.New("unknown nested content type")
		}
		if err != nil {
			return nil, fmt.Errorf("content part %d: %w", i, err)
		}
		out.Content[i] = p
	}
	return out, nil
}

func selectedResultVariant(b *schema.FunctionToolResultContentBlock) (schema.FunctionToolResultContentBlockType, int) {
	variants := []struct {
		kind schema.FunctionToolResultContentBlockType
		set  bool
	}{
		{schema.FunctionToolResultContentBlockTypeText, b.Text != nil}, {schema.FunctionToolResultContentBlockTypeImage, b.Image != nil},
		{schema.FunctionToolResultContentBlockTypeAudio, b.Audio != nil}, {schema.FunctionToolResultContentBlockTypeVideo, b.Video != nil},
		{schema.FunctionToolResultContentBlockTypeFile, b.File != nil},
	}
	var selected schema.FunctionToolResultContentBlockType
	count := 0
	for _, v := range variants {
		if v.set {
			selected = v.kind
			count++
		}
	}
	return selected, count
}

type toolInfoWire struct {
	Name       string                           `json:"name"`
	Desc       string                           `json:"desc"`
	Has        bool                             `json:"has_params_one_of"`
	Params     map[string]*schema.ParameterInfo `json:"params"`
	JSONSchema json.RawMessage                  `json:"json_schema"`
}

func projectToolDefinition(v *schema.ToolInfo, limits ProjectionLimits) (PublicToolDefinition, error) {
	if v == nil {
		return PublicToolDefinition{}, errors.New("tool definition is nil")
	}
	sanitized := *v
	sanitized.Extra = nil
	b, err := json.Marshal(&sanitized)
	if err != nil {
		return PublicToolDefinition{}, errors.New("cannot inspect tool parameters")
	}
	var wire toolInfoWire
	if err := json.Unmarshal(b, &wire); err != nil {
		return PublicToolDefinition{}, errors.New("cannot inspect tool parameters")
	}
	out := PublicToolDefinition{Name: v.Name, Description: v.Desc, ParamsKind: "none"}
	if !wire.Has {
		return out, nil
	}
	if wire.Params != nil && len(wire.JSONSchema) != 0 {
		return out, errors.New("tool has both parameter representations")
	}
	if len(wire.JSONSchema) != 0 && string(wire.JSONSchema) != "null" {
		if err := validateToolJSONSchema(wire.JSONSchema, limits); err != nil {
			return out, err
		}
		out.ParamsKind = "json_schema"
		out.JSONSchema = append(json.RawMessage(nil), wire.JSONSchema...)
		return out, nil
	}
	out.ParamsKind = "params"
	out.Params = make(map[string]*PublicParameterInfo, len(wire.Params))
	entries := 0
	for k, p := range wire.Params {
		if k == "" {
			return out, errors.New("parameter name is empty")
		}
		if err := addBoundedCount(&entries, 1, limits.MaxJSONEntries, "parameter entry limit exceeded"); err != nil {
			return out, err
		}
		cloned, err := cloneParameter(p, 1, limits, &entries, map[*schema.ParameterInfo]bool{})
		if err != nil {
			return out, fmt.Errorf("parameter %s: %w", k, err)
		}
		out.Params[k] = cloned
	}
	return out, nil
}

func validateToolJSONSchema(data json.RawMessage, limits ProjectionLimits) error {
	if len(data) > limits.MaxBlockBytes {
		return errors.New("JSON schema byte limit exceeded")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return errors.New("cannot decode JSON schema")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("cannot decode JSON schema")
	}
	entries := 0
	if err := walkJSON(reflect.ValueOf(value), 1, limits, &entries, map[visit]bool{}); err != nil {
		return fmt.Errorf("JSON schema: %w", err)
	}
	return nil
}

func cloneParameter(v *schema.ParameterInfo, depth int, limits ProjectionLimits, entries *int, stack map[*schema.ParameterInfo]bool) (*PublicParameterInfo, error) {
	if v == nil {
		return nil, errors.New("is nil")
	}
	if err := validateParameterShape(v.Type, v.ElemInfo != nil, v.SubParams != nil, len(v.Enum) != 0); err != nil {
		return nil, err
	}
	if depth > limits.MaxJSONDepth {
		return nil, errors.New("depth limit exceeded")
	}
	if stack[v] {
		return nil, errors.New("contains a cycle")
	}
	if err := addBoundedCount(entries, len(v.Enum), limits.MaxJSONEntries, "entry limit exceeded"); err != nil {
		return nil, err
	}
	if err := addBoundedCount(entries, len(v.SubParams), limits.MaxJSONEntries, "entry limit exceeded"); err != nil {
		return nil, err
	}
	stack[v] = true
	defer delete(stack, v)
	out := &PublicParameterInfo{Type: v.Type, Desc: v.Desc, Enum: append([]string(nil), v.Enum...), Required: v.Required}
	var err error
	if v.ElemInfo != nil {
		out.ElemInfo, err = cloneParameter(v.ElemInfo, depth+1, limits, entries, stack)
		if err != nil {
			return nil, err
		}
	}
	if v.SubParams != nil {
		out.SubParams = make(map[string]*PublicParameterInfo, len(v.SubParams))
		for k, p := range v.SubParams {
			if k == "" {
				return nil, errors.New("nested parameter name is empty")
			}
			out.SubParams[k], err = cloneParameter(p, depth+1, limits, entries, stack)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func addBoundedCount(total *int, next, limit int, message string) error {
	if next < 0 || *total > limit-next {
		return errors.New(message)
	}
	*total += next
	return nil
}

func projectToolSearch(v *schema.ToolSearchFunctionToolResult, limits ProjectionLimits) (*PublicToolSearchResult, error) {
	if v.CallID == "" || v.Name == "" {
		return nil, errors.New("tool search call ID and name are required")
	}
	if v.Result == nil {
		return nil, errors.New("tool search result is required")
	}
	if len(v.Result.Tools) > limits.MaxToolDefinitions {
		return nil, errors.New("tool definition limit exceeded")
	}
	out := &PublicToolSearchResult{CallID: v.CallID, Name: v.Name, Tools: make([]PublicToolDefinition, len(v.Result.Tools))}
	seen := map[string]bool{}
	for i, tool := range v.Result.Tools {
		p, err := projectToolDefinition(tool, limits)
		if err != nil {
			return nil, fmt.Errorf("tool %d: %w", i, err)
		}
		if p.Name == "" || seen[p.Name] {
			return nil, fmt.Errorf("tool %d has empty or duplicate name", i)
		}
		seen[p.Name] = true
		out.Tools[i] = p
	}
	return out, nil
}

func projectMCPList(v *schema.MCPListToolsResult, limits ProjectionLimits) (*PublicMCPListToolsResult, error) {
	if v.ServerLabel == "" {
		return nil, errors.New("MCP server label is required")
	}
	if len(v.Tools) > limits.MaxToolDefinitions {
		return nil, errors.New("tool definition limit exceeded")
	}
	out := &PublicMCPListToolsResult{ServerLabel: v.ServerLabel, Error: v.Error, Tools: make([]PublicMCPListToolsItem, len(v.Tools))}
	seen := map[string]bool{}
	for i, tool := range v.Tools {
		if tool == nil || tool.Name == "" || seen[tool.Name] {
			return nil, fmt.Errorf("tool %d is nil, empty, or duplicate", i)
		}
		seen[tool.Name] = true
		item := PublicMCPListToolsItem{Name: tool.Name, Description: tool.Description}
		if tool.InputSchema != nil {
			b, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, errors.New("cannot encode input schema")
			}
			item.InputSchema = b
		}
		out.Tools[i] = item
	}
	return out, nil
}

func projectTextAnnotations(v *schema.AssistantGenText, limits ProjectionLimits) (*PublicProviderAnnotations, error) {
	out := &PublicProviderAnnotations{}
	annotationCount := 0
	if v.OpenAIExtension != nil {
		if v.OpenAIExtension.Refusal != nil {
			out.RefusalReason = v.OpenAIExtension.Refusal.Reason
		}
		if err := addBoundedCount(&annotationCount, len(v.OpenAIExtension.Annotations), limits.MaxAnnotations, "annotation limit exceeded"); err != nil {
			return nil, errors.New("annotation limit exceeded")
		}
		for i, a := range v.OpenAIExtension.Annotations {
			p, err := projectOpenAIAnnotation(a, v.Text)
			if err != nil {
				return nil, fmt.Errorf("OpenAI annotation %d: %w", i, err)
			}
			out.OpenAI = append(out.OpenAI, p)
		}
	}
	if v.ClaudeExtension != nil {
		if err := addBoundedCount(&annotationCount, len(v.ClaudeExtension.Citations), limits.MaxAnnotations, "annotation limit exceeded"); err != nil {
			return nil, errors.New("citation limit exceeded")
		}
		for i, c := range v.ClaudeExtension.Citations {
			p, err := projectClaudeCitation(c)
			if err != nil {
				return nil, fmt.Errorf("claude citation %d: %w", i, err)
			}
			out.Claude = append(out.Claude, p)
		}
	}
	if out.RefusalReason == "" && len(out.OpenAI) == 0 && len(out.Claude) == 0 {
		return nil, nil
	}
	if err := validatePublicProviderAnnotations(out, v.Text); err != nil {
		return nil, err
	}
	return out, nil
}

func projectOpenAIAnnotation(a *openai.TextAnnotation, text string) (PublicOpenAIAnnotation, error) {
	if a == nil {
		return PublicOpenAIAnnotation{}, errors.New("is nil")
	}
	count := boolCount(a.FileCitation != nil, a.URLCitation != nil, a.ContainerFileCitation != nil, a.FilePath != nil)
	if count != 1 {
		return PublicOpenAIAnnotation{}, errors.New("must select exactly one variant")
	}
	out := PublicOpenAIAnnotation{Type: string(a.Type)}
	switch a.Type {
	case openai.TextAnnotationTypeFileCitation:
		if a.FileCitation == nil {
			return out, errors.New("type mismatch")
		}
		out.FileID, out.Filename, out.Index = a.FileCitation.FileID, a.FileCitation.Filename, a.FileCitation.Index
		if out.Index < 0 {
			return out, errors.New("negative index")
		}
	case openai.TextAnnotationTypeURLCitation:
		if a.URLCitation == nil {
			return out, errors.New("type mismatch")
		}
		out.Title, out.URL, out.StartIndex, out.EndIndex = a.URLCitation.Title, a.URLCitation.URL, a.URLCitation.StartIndex, a.URLCitation.EndIndex
		if err := validateTextRange(text, out.StartIndex, out.EndIndex); err != nil {
			return out, err
		}
	case openai.TextAnnotationTypeContainerFileCitation:
		if a.ContainerFileCitation == nil {
			return out, errors.New("type mismatch")
		}
		x := a.ContainerFileCitation
		out.ContainerID, out.FileID, out.Filename, out.StartIndex, out.EndIndex = x.ContainerID, x.FileID, x.Filename, x.StartIndex, x.EndIndex
		if err := validateTextRange(text, out.StartIndex, out.EndIndex); err != nil {
			return out, err
		}
	case openai.TextAnnotationTypeFilePath:
		if a.FilePath == nil {
			return out, errors.New("type mismatch")
		}
		out.FileID, out.Index = a.FilePath.FileID, a.FilePath.Index
		if out.Index < 0 {
			return out, errors.New("negative index")
		}
	default:
		return out, errors.New("unknown annotation type")
	}
	return out, nil
}

func projectClaudeCitation(c *claude.TextCitation) (PublicClaudeCitation, error) {
	if c == nil {
		return PublicClaudeCitation{}, errors.New("is nil")
	}
	count := boolCount(c.CharLocation != nil, c.PageLocation != nil, c.ContentBlockLocation != nil, c.WebSearchResultLocation != nil)
	if count != 1 {
		return PublicClaudeCitation{}, errors.New("must select exactly one variant")
	}
	out := PublicClaudeCitation{Type: string(c.Type)}
	switch c.Type {
	case claude.TextCitationTypeCharLocation:
		if c.CharLocation == nil {
			return out, errors.New("type mismatch")
		}
		x := c.CharLocation
		out.CitedText, out.DocumentTitle, out.DocumentIndex, out.StartIndex, out.EndIndex = x.CitedText, x.DocumentTitle, x.DocumentIndex, x.StartCharIndex, x.EndCharIndex
	case claude.TextCitationTypePageLocation:
		if c.PageLocation == nil {
			return out, errors.New("type mismatch")
		}
		x := c.PageLocation
		out.CitedText, out.DocumentTitle, out.DocumentIndex, out.StartIndex, out.EndIndex = x.CitedText, x.DocumentTitle, x.DocumentIndex, x.StartPageNumber, x.EndPageNumber
	case claude.TextCitationTypeContentBlockLocation:
		if c.ContentBlockLocation == nil {
			return out, errors.New("type mismatch")
		}
		x := c.ContentBlockLocation
		out.CitedText, out.DocumentTitle, out.DocumentIndex, out.StartIndex, out.EndIndex = x.CitedText, x.DocumentTitle, x.DocumentIndex, x.StartBlockIndex, x.EndBlockIndex
	case claude.TextCitationTypeWebSearchResultLocation:
		if c.WebSearchResultLocation == nil {
			return out, errors.New("type mismatch")
		}
		x := c.WebSearchResultLocation
		out.CitedText, out.Title, out.URL = x.CitedText, x.Title, x.URL
		return out, nil
	default:
		return out, errors.New("unknown citation type")
	}
	if out.DocumentIndex < 0 || out.StartIndex < 0 || out.EndIndex < out.StartIndex {
		return out, errors.New("invalid source range")
	}
	return out, nil
}

func projectResponseMeta(v *schema.AgenticResponseMeta, blocks []PublicContentBlock, limits ProjectionLimits) (*PublicResponseMeta, error) {
	if v == nil {
		return nil, nil
	}
	out := &PublicResponseMeta{}
	var err error
	out.TokenUsage, err = ToAGUITokenUsage(v.TokenUsage, "", "")
	if err != nil {
		return nil, projectErr(-1, "responseMeta.tokenUsage", err.Error())
	}
	if v.OpenAIExtension != nil {
		x := v.OpenAIExtension
		out.OpenAIStatus = string(x.Status)
		if x.Error != nil {
			out.OpenAIError = &PublicProviderError{Code: string(x.Error.Code), Message: x.Error.Message}
		}
		if x.IncompleteDetails != nil {
			out.OpenAIIncompleteReason = x.IncompleteDetails.Reason
		}
	}
	if v.ClaudeExtension != nil {
		x := v.ClaudeExtension
		out.ClaudeStopReason, out.ClaudeStopSequence = x.StopReason, x.StopSequence
		if x.StopDetails != nil {
			out.ClaudeStopCategory, out.ClaudeStopExplanation = x.StopDetails.Category, x.StopDetails.Explanation
		}
	}
	if v.GeminiExtension != nil {
		out.GeminiFinishReason = v.GeminiExtension.FinishReason
		if v.GeminiExtension.GroundingMeta != nil {
			out.GeminiGrounding, err = projectGemini(v.GeminiExtension.GroundingMeta, blocks, limits)
			if err != nil {
				return nil, projectErr(-1, "responseMeta.geminiGrounding", err.Error())
			}
		}
	}
	if out.TokenUsage == nil && out.OpenAIStatus == "" && out.OpenAIError == nil && out.OpenAIIncompleteReason == "" && out.ClaudeStopReason == "" && out.ClaudeStopSequence == "" && out.ClaudeStopCategory == "" && out.ClaudeStopExplanation == "" && out.GeminiFinishReason == "" && out.GeminiGrounding == nil {
		return nil, nil
	}
	if err := validatePublicResponseMeta(out); err != nil {
		return nil, err
	}
	return out, nil
}

func projectGemini(v *gemini.GroundingMetadata, blocks []PublicContentBlock, limits ProjectionLimits) (*PublicGeminiGrounding, error) {
	if v == nil {
		return nil, nil
	}
	annotationCount := 0
	if err := addBoundedCount(&annotationCount, len(v.GroundingChunks), limits.MaxAnnotations, "grounding entry limit exceeded"); err != nil {
		return nil, err
	}
	if err := addBoundedCount(&annotationCount, len(v.GroundingSupports), limits.MaxAnnotations, "grounding entry limit exceeded"); err != nil {
		return nil, err
	}
	entries := 0
	if err := addBoundedCount(&entries, len(v.WebSearchQueries), limits.MaxJSONEntries, "grounding value entry limit exceeded"); err != nil {
		return nil, err
	}
	out := &PublicGeminiGrounding{WebSearchQueries: append([]string(nil), v.WebSearchQueries...)}
	if v.SearchEntryPoint != nil {
		out.RenderedSearchContent = v.SearchEntryPoint.RenderedContent
	}
	for i, chunk := range v.GroundingChunks {
		if chunk == nil || chunk.Web == nil {
			return nil, fmt.Errorf("chunk %d has no web variant", i)
		}
		out.Chunks = append(out.Chunks, PublicGeminiGroundingChunk{Domain: chunk.Web.Domain, Title: chunk.Web.Title, URI: chunk.Web.URI})
	}
	for i, support := range v.GroundingSupports {
		if support == nil || support.Segment == nil {
			return nil, fmt.Errorf("support %d has no segment", i)
		}
		s := support.Segment
		if s.PartIndex < 0 || s.PartIndex >= len(blocks) || blocks[s.PartIndex].Type != schema.ContentBlockTypeAssistantGenText || blocks[s.PartIndex].Text == nil {
			return nil, fmt.Errorf("support %d targets a non-text block", i)
		}
		if err := validateTextRange(*blocks[s.PartIndex].Text, s.StartIndex, s.EndIndex); err != nil {
			return nil, fmt.Errorf("support %d: %w", i, err)
		}
		if s.Text != (*blocks[s.PartIndex].Text)[s.StartIndex:s.EndIndex] {
			return nil, fmt.Errorf("support %d text does not match its target range", i)
		}
		if err := addBoundedCount(&entries, len(support.GroundingChunkIndices), limits.MaxJSONEntries, "grounding value entry limit exceeded"); err != nil {
			return nil, err
		}
		if err := addBoundedCount(&entries, len(support.ConfidenceScores), limits.MaxJSONEntries, "grounding value entry limit exceeded"); err != nil {
			return nil, err
		}
		for _, idx := range support.GroundingChunkIndices {
			if idx < 0 || idx >= len(out.Chunks) {
				return nil, fmt.Errorf("support %d has invalid chunk index", i)
			}
		}
		for _, score := range support.ConfidenceScores {
			if math.IsNaN(float64(score)) || math.IsInf(float64(score), 0) || score < 0 || score > 1 {
				return nil, fmt.Errorf("support %d has invalid confidence score", i)
			}
		}
		out.Supports = append(out.Supports, PublicGeminiGroundingSupport{ConfidenceScores: append([]float32(nil), support.ConfidenceScores...), GroundingChunkIndices: append([]int(nil), support.GroundingChunkIndices...), PartIndex: s.PartIndex, StartIndex: s.StartIndex, EndIndex: s.EndIndex, Text: s.Text})
	}
	return out, nil
}

func validateTextRange(text string, start, end int) error {
	if !utf8.ValidString(text) || start < 0 || end < start || end > len(text) || (start < len(text) && !utf8.RuneStart(text[start])) || (end < len(text) && !utf8.RuneStart(text[end])) {
		return errors.New("invalid UTF-8 byte range")
	}
	return nil
}
func boolCount(values ...bool) int {
	n := 0
	for _, v := range values {
		if v {
			n++
		}
	}
	return n
}
func stringPtr(v string) *string { return &v }
func cloneIdentity(v AgenticIdentityV1) AgenticIdentityV1 {
	v.AgentPath = append([]AgentPathSegment(nil), v.AgentPath...)
	return v
}

func clonePublicJSON(v any, limits ProjectionLimits) (any, error) {
	if v == nil {
		return nil, nil
	}
	entries := 0
	if err := walkJSON(reflect.ValueOf(v), 1, limits, &entries, map[visit]bool{}); err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, errors.New("value is not JSON-compatible")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, errors.New("value is not JSON-compatible")
	}
	return out, nil
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

func walkJSON(v reflect.Value, depth int, limits ProjectionLimits, entries *int, stack map[visit]bool) error {
	if !v.IsValid() {
		return nil
	}
	if depth > limits.MaxJSONDepth {
		return errors.New("JSON depth limit exceeded")
	}
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		key := visit{v.Type(), v.Pointer()}
		if stack[key] {
			return errors.New("cyclic JSON value")
		}
		stack[key] = true
		defer delete(stack, key)
		return walkJSON(v.Elem(), depth+1, limits, entries, stack)
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return errors.New("JSON object keys must be strings")
		}
		if v.IsNil() {
			return nil
		}
		key := visit{v.Type(), v.Pointer()}
		if stack[key] {
			return errors.New("cyclic JSON value")
		}
		stack[key] = true
		defer delete(stack, key)
		if err := addBoundedCount(entries, v.Len(), limits.MaxJSONEntries, "JSON entry limit exceeded"); err != nil {
			return err
		}
		iter := v.MapRange()
		for iter.Next() {
			if err := walkJSON(iter.Value(), depth+1, limits, entries, stack); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice:
		if v.IsNil() {
			return nil
		}
		key := visit{v.Type(), v.Pointer()}
		if v.Pointer() != 0 && stack[key] {
			return errors.New("cyclic JSON value")
		}
		stack[key] = true
		defer delete(stack, key)
		fallthrough
	case reflect.Array:
		if err := addBoundedCount(entries, v.Len(), limits.MaxJSONEntries, "JSON entry limit exceeded"); err != nil {
			return err
		}
		for i := 0; i < v.Len(); i++ {
			if err := walkJSON(v.Index(i), depth+1, limits, entries, stack); err != nil {
				return err
			}
		}
		return nil
	case reflect.String, reflect.Bool:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return nil
	case reflect.Float32, reflect.Float64:
		if math.IsInf(v.Float(), 0) || math.IsNaN(v.Float()) {
			return errors.New("non-finite JSON number")
		}
		return nil
	default:
		return fmt.Errorf("unsupported JSON value type %s", v.Kind())
	}
}

func ProjectionDigestV1(value *PublicAgenticMessage) (CandidateDigestV1, error) {
	if value == nil {
		return "", errors.New("projection is nil")
	}
	if err := validatePublicAgenticMessage(value); err != nil {
		return "", err
	}
	copy := *value
	copy.Digest = ""
	return digestCanonical("eino-agentic-v1\x00projection\x00", &copy)
}
func LifecycleDigestV1(value *AgenticEnvelopeV1) (CandidateDigestV1, error) {
	if value == nil {
		return "", errors.New("envelope is nil")
	}
	if err := validateEnvelope(value, false); err != nil {
		return "", err
	}
	copy := *value
	copy.Digest = ""
	return digestCanonical("eino-agentic-v1\x00lifecycle\x00"+string(value.Kind)+"\x00", &copy)
}
func digestCanonical(prefix string, value any) (CandidateDigestV1, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := jsoncanonicalizer.Transform(b)
	if err != nil {
		return "", fmt.Errorf("canonicalize JSON: %w", err)
	}
	sum := sha256.Sum256(append([]byte(prefix), canonical...))
	return CandidateDigestV1(hex.EncodeToString(sum[:])), nil
}

func DecodeAgenticEnvelope(value any) (*AgenticEnvelopeV1, error) {
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = append([]byte(nil), v...)
	case json.RawMessage:
		data = append([]byte(nil), v...)
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(value)
		if err != nil {
			return nil, errors.New("agentic envelope is not JSON-compatible")
		}
	}
	if len(data) > DefaultProjectionLimits().MaxMessageBytes {
		return nil, errors.New("agentic envelope exceeds the public message limit")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var out AgenticEnvelopeV1
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode agentic envelope: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, err
	}
	if err := ValidateAgenticEnvelope(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
func ensureEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("decode agentic envelope: trailing JSON value")
	}
	return err
}
func ValidateAgenticEnvelope(value *AgenticEnvelopeV1) error { return validateEnvelope(value, true) }
func validateEnvelope(value *AgenticEnvelopeV1, verifyDigest bool) error {
	if value == nil || value.Version != AgenticSchemaVersion {
		return errors.New("agentic envelope has unsupported version")
	}
	if err := validateIdentity(value.Identity, DefaultProjectionLimits().MaxAgentPathSegments, value.Kind == EnvelopeContentBlock); err != nil {
		return fmt.Errorf("agentic envelope identity: %w", err)
	}
	selected := boolCount(value.ContentBlock != nil, value.ResponseMeta != nil, value.Lifecycle != nil, value.AttemptReplaced != nil, value.Paused != nil, value.Resumed != nil, value.Cancelled != nil)
	if selected != 1 {
		return errors.New("agentic envelope must contain exactly one payload")
	}
	valid := false
	switch value.Kind {
	case EnvelopeContentBlock:
		valid = value.ContentBlock != nil
	case EnvelopeResponseMeta:
		valid = value.ResponseMeta != nil
	case EnvelopeAttemptReplaced:
		valid = value.AttemptReplaced != nil
	case EnvelopePaused:
		valid = value.Paused != nil
	case EnvelopeResumed:
		valid = value.Resumed != nil
	case EnvelopeCancelled:
		valid = value.Cancelled != nil
	case EnvelopeRunStarted, EnvelopeRunFinished, EnvelopeRunError, EnvelopeTurnStarted, EnvelopeTurnFinished, EnvelopeSubagentStarted, EnvelopeSubagentFinished, EnvelopeSubagentError:
		valid = value.Lifecycle != nil
	}
	if !valid {
		return errors.New("agentic envelope kind does not match payload")
	}
	if value.ContentBlock != nil && !reflect.DeepEqual(value.ContentBlock.Identity, value.Identity) {
		return errors.New("agentic envelope content identity does not match envelope identity")
	}
	if value.ContentBlock != nil {
		if err := validatePublicContentBlock(value.ContentBlock); err != nil {
			return fmt.Errorf("agentic envelope content block: %w", err)
		}
	}
	if value.ResponseMeta != nil {
		if err := validatePublicResponseMeta(value.ResponseMeta); err != nil {
			return fmt.Errorf("agentic envelope response metadata: %w", err)
		}
	}
	if value.AttemptReplaced != nil {
		if value.AttemptReplaced.OldAttemptID == "" || value.AttemptReplaced.NewAttemptID == "" || value.AttemptReplaced.OldAttemptID == value.AttemptReplaced.NewAttemptID {
			return errors.New("attempt replacement requires distinct old and new attempt IDs")
		}
		if value.AttemptReplaced.Cause == "" || value.AttemptReplaced.Semantics == "" {
			return errors.New("attempt replacement requires cause and semantics")
		}
		if value.Identity.AttemptID != value.AttemptReplaced.OldAttemptID {
			return errors.New("attempt replacement identity must name the old attempt")
		}
	}
	if value.Paused != nil {
		if value.Paused.PauseID == "" {
			return errors.New("pause ID is required")
		}
		if err := validateInterruptTargets(value.Paused.Targets); err != nil {
			return err
		}
		if err := validateCorrelation(value.Paused.Correlation, value.Paused.Targets); err != nil {
			return err
		}
	}
	if value.Resumed != nil {
		if value.Resumed.PauseID == "" || value.Resumed.NewTurnID == "" || value.Resumed.NewAttemptID == "" {
			return errors.New("resume requires pause, new turn, and new attempt IDs")
		}
		if err := validateInterruptTargets(value.Resumed.Targets); err != nil {
			return err
		}
		if err := validateCorrelation(value.Resumed.Correlation, value.Resumed.Targets); err != nil {
			return err
		}
		if value.Resumed.NewTurnID == value.Identity.TurnID || value.Resumed.NewAttemptID == value.Identity.AttemptID {
			return errors.New("resume requires new turn and attempt IDs")
		}
	}
	if value.Cancelled != nil {
		if err := validateCancellation(value.Cancelled); err != nil {
			return err
		}
	}
	if verifyDigest {
		if value.Digest == "" {
			return errors.New("agentic envelope digest is required")
		}
		want, err := LifecycleDigestV1(value)
		if err != nil {
			return err
		}
		if !strings.EqualFold(string(value.Digest), string(want)) {
			return errors.New("agentic envelope digest mismatch")
		}
		if string(value.Digest) != strings.ToLower(string(value.Digest)) {
			return errors.New("agentic envelope digest must be lowercase")
		}
	}
	return nil
}

func validateCancellation(cancelled *CancelledV1) error {
	if cancelled == nil || !validCancellationMode(cancelled.RequestedMode) || !validCancellationMode(cancelled.ObservedMode) {
		return errors.New("cancellation modes are invalid")
	}
	switch cancelled.Classification {
	case CancellationClassImmediate:
		if cancelled.RequestedMode != CancellationModeImmediate || cancelled.ObservedMode != CancellationModeImmediate {
			return errors.New("immediate cancellation requires immediate requested and observed modes")
		}
	case CancellationClassSafePoint:
		if cancelled.RequestedMode == CancellationModeImmediate || cancelled.ObservedMode != cancelled.RequestedMode {
			return errors.New("safe-point cancellation requires one matching graceful mode")
		}
	case CancellationClassEscalated, CancellationClassTimeout:
		if cancelled.RequestedMode == CancellationModeImmediate || cancelled.ObservedMode != CancellationModeImmediate {
			return errors.New("escalated cancellation requires a graceful request and immediate observation")
		}
	default:
		return errors.New("cancellation classification is invalid")
	}
	return nil
}

func validCancellationMode(mode CancellationModeV1) bool {
	switch mode {
	case CancellationModeImmediate, CancellationModeAfterChatModel, CancellationModeAfterToolCalls, CancellationModeAfterChatOrToolCalls:
		return true
	default:
		return false
	}
}

func validatePublicContentBlock(block *PublicContentBlock) error {
	if block == nil {
		return errors.New("content block is required")
	}
	selected := boolCount(
		block.Text != nil, block.Media != nil, block.ToolSearchResult != nil,
		block.FunctionToolCall != nil, block.FunctionToolResult != nil,
		block.ServerToolCall != nil, block.ServerToolResult != nil,
		block.MCPToolCall != nil, block.MCPToolResult != nil,
		block.MCPListToolsResult != nil, block.MCPApprovalRequest != nil,
		block.MCPApprovalResponse != nil,
	)
	if selected != 1 {
		return errors.New("content discriminator must select exactly one payload")
	}
	valid := false
	switch block.Type {
	case schema.ContentBlockTypeReasoning, schema.ContentBlockTypeUserInputText, schema.ContentBlockTypeAssistantGenText:
		valid = block.Text != nil
	case schema.ContentBlockTypeUserInputImage, schema.ContentBlockTypeUserInputAudio,
		schema.ContentBlockTypeUserInputVideo, schema.ContentBlockTypeUserInputFile,
		schema.ContentBlockTypeAssistantGenImage, schema.ContentBlockTypeAssistantGenAudio,
		schema.ContentBlockTypeAssistantGenVideo:
		valid = block.Media != nil && validatePublicMedia(block.Media) == nil
	case schema.ContentBlockTypeToolSearchResult:
		valid = block.ToolSearchResult != nil && block.ToolSearchResult.CallID != "" && block.ToolSearchResult.Name != ""
	case schema.ContentBlockTypeFunctionToolCall:
		valid = block.FunctionToolCall != nil && block.FunctionToolCall.CallID != "" && block.FunctionToolCall.Name != ""
	case schema.ContentBlockTypeFunctionToolResult:
		valid = validatePublicFunctionResult(block.FunctionToolResult) == nil
	case schema.ContentBlockTypeServerToolCall:
		valid = block.ServerToolCall != nil && block.ServerToolCall.ProviderServerID != "" && block.ServerToolCall.CallID != "" && block.ServerToolCall.Name != "" && block.ServerToolCall.ExecutionOwner == ToolExecutionOwnerProvider
	case schema.ContentBlockTypeServerToolResult:
		valid = block.ServerToolResult != nil && block.ServerToolResult.ProviderServerID != "" && block.ServerToolResult.CallID != "" && block.ServerToolResult.Name != "" && block.ServerToolResult.ExecutionOwner == ToolExecutionOwnerProvider
	case schema.ContentBlockTypeMCPToolCall:
		valid = block.MCPToolCall != nil && block.MCPToolCall.ServerLabel != "" && block.MCPToolCall.CallID != "" && block.MCPToolCall.Name != "" && block.MCPToolCall.ExecutionOwner == ToolExecutionOwnerProviderMCP
	case schema.ContentBlockTypeMCPToolResult:
		valid = block.MCPToolResult != nil && block.MCPToolResult.ServerLabel != "" && block.MCPToolResult.CallID != "" && block.MCPToolResult.Name != "" && block.MCPToolResult.ExecutionOwner == ToolExecutionOwnerProviderMCP
	case schema.ContentBlockTypeMCPListToolsResult:
		valid = block.MCPListToolsResult != nil && block.MCPListToolsResult.ServerLabel != ""
	case schema.ContentBlockTypeMCPToolApprovalRequest:
		valid = block.MCPApprovalRequest != nil && block.MCPApprovalRequest.ID != "" && block.MCPApprovalRequest.ServerLabel != "" && block.MCPApprovalRequest.Name != ""
	case schema.ContentBlockTypeMCPToolApprovalResponse:
		valid = block.MCPApprovalResponse != nil && block.MCPApprovalResponse.ApprovalRequestID != ""
	}
	if !valid {
		return errors.New("content kind does not match a complete payload")
	}
	limits := DefaultProjectionLimits()
	encoded, err := json.Marshal(block)
	if err != nil || len(encoded) > limits.MaxBlockBytes {
		return errors.New("content block exceeds the public block limit")
	}
	switch block.Type {
	case schema.ContentBlockTypeToolSearchResult:
		if len(block.ToolSearchResult.Tools) > limits.MaxToolDefinitions {
			return errors.New("tool definition limit exceeded")
		}
		for i := range block.ToolSearchResult.Tools {
			if err := validatePublicToolDefinition(&block.ToolSearchResult.Tools[i], limits); err != nil {
				return fmt.Errorf("tool %d: %w", i, err)
			}
		}
	case schema.ContentBlockTypeFunctionToolResult:
		if len(block.FunctionToolResult.Content) > limits.MaxBlocks {
			return errors.New("function result part limit exceeded")
		}
	case schema.ContentBlockTypeServerToolCall:
		if _, err := clonePublicJSON(block.ServerToolCall.Arguments, limits); err != nil {
			return fmt.Errorf("server tool arguments: %w", err)
		}
	case schema.ContentBlockTypeServerToolResult:
		if _, err := clonePublicJSON(block.ServerToolResult.Content, limits); err != nil {
			return fmt.Errorf("server tool content: %w", err)
		}
	case schema.ContentBlockTypeMCPListToolsResult:
		if len(block.MCPListToolsResult.Tools) > limits.MaxToolDefinitions {
			return errors.New("MCP tool definition limit exceeded")
		}
	}
	if block.ProviderAnnotations != nil && block.Type != schema.ContentBlockTypeAssistantGenText {
		return errors.New("provider annotations require assistant generated text")
	}
	if block.ProviderAnnotations != nil {
		if err := validatePublicProviderAnnotations(block.ProviderAnnotations, derefString(block.Text)); err != nil {
			return err
		}
	}
	wantCallID := publicBlockCallID(block)
	if wantCallID == "" {
		if block.Identity.CallID != "" {
			return errors.New("content identity has an unexpected call ID")
		}
	} else if block.Identity.CallID != wantCallID {
		return errors.New("content call ID does not match identity")
	}
	return nil
}

func validatePublicAgenticMessage(message *PublicAgenticMessage) error {
	if message == nil || message.Version != AgenticSchemaVersion {
		return errors.New("agentic projection has unsupported version")
	}
	limits := DefaultProjectionLimits()
	if err := validateIdentity(message.Identity, limits.MaxAgentPathSegments, false); err != nil {
		return fmt.Errorf("agentic projection identity: %w", err)
	}
	if message.Identity.BlockID != "" || message.Identity.CallID != "" || message.Identity.Transient {
		return errors.New("agentic projection base identity cannot be block-scoped or transient")
	}
	if message.Role != schema.AgenticRoleTypeSystem && message.Role != schema.AgenticRoleTypeUser && message.Role != schema.AgenticRoleTypeAssistant {
		return errors.New("agentic projection role is invalid")
	}
	if len(message.ContentBlocks) > limits.MaxBlocks {
		return errors.New("agentic projection block limit exceeded")
	}
	seenBlocks := make(map[string]struct{}, len(message.ContentBlocks))
	seenOwnedIDs := make(map[string]struct{}, len(message.ContentBlocks))
	for i := range message.ContentBlocks {
		block := &message.ContentBlocks[i]
		if err := validatePublicContentBlock(block); err != nil {
			return fmt.Errorf("agentic projection block %d: %w", i, err)
		}
		if err := validateOwnedIDUnique(block, seenOwnedIDs); err != nil {
			return fmt.Errorf("agentic projection block %d: %w", i, err)
		}
		if !roleAllowsBlock(message.Role, block.Type) {
			return fmt.Errorf("agentic projection block %d is incompatible with message role", i)
		}
		if _, duplicate := seenBlocks[block.Identity.BlockID]; duplicate {
			return fmt.Errorf("agentic projection block %d has a duplicate block ID", i)
		}
		seenBlocks[block.Identity.BlockID] = struct{}{}
		expected := cloneIdentity(message.Identity)
		expected.BlockID = block.Identity.BlockID
		expected.CallID = publicBlockCallID(block)
		if !reflect.DeepEqual(block.Identity, expected) {
			return fmt.Errorf("agentic projection block %d identity does not match its message", i)
		}
	}
	if message.ResponseMeta != nil {
		if err := validatePublicResponseMeta(message.ResponseMeta); err != nil {
			return fmt.Errorf("agentic projection response metadata: %w", err)
		}
		if err := validatePublicGroundingTargets(message.ResponseMeta.GeminiGrounding, message.ContentBlocks); err != nil {
			return fmt.Errorf("agentic projection response metadata: %w", err)
		}
	}
	copyMessage := *message
	copyMessage.Digest = ""
	encoded, err := json.Marshal(&copyMessage)
	if err != nil || len(encoded) > limits.MaxMessageBytes {
		return errors.New("agentic projection encoded message limit exceeded")
	}
	return nil
}

func validateOwnedIDUnique(block *PublicContentBlock, seen map[string]struct{}) error {
	domain, id := publicBlockOwnedID(block)
	if id == "" {
		return nil
	}
	key := domain + "\x00" + id
	if _, duplicate := seen[key]; duplicate {
		return fmt.Errorf("duplicate %s identity", domain)
	}
	seen[key] = struct{}{}
	return nil
}

func publicBlockOwnedID(block *PublicContentBlock) (string, string) {
	switch block.Type {
	case schema.ContentBlockTypeFunctionToolCall, schema.ContentBlockTypeServerToolCall, schema.ContentBlockTypeMCPToolCall:
		return "call proposal", publicBlockCallID(block)
	case schema.ContentBlockTypeToolSearchResult, schema.ContentBlockTypeFunctionToolResult, schema.ContentBlockTypeServerToolResult, schema.ContentBlockTypeMCPToolResult:
		return "call result", publicBlockCallID(block)
	case schema.ContentBlockTypeMCPToolApprovalRequest:
		if block.MCPApprovalRequest != nil {
			return "approval request", block.MCPApprovalRequest.ID
		}
	case schema.ContentBlockTypeMCPToolApprovalResponse:
		if block.MCPApprovalResponse != nil {
			return "approval response", block.MCPApprovalResponse.ApprovalRequestID
		}
	}
	return "", ""
}

func validatePublicGroundingTargets(grounding *PublicGeminiGrounding, blocks []PublicContentBlock) error {
	if grounding == nil {
		return nil
	}
	for i, support := range grounding.Supports {
		if support.PartIndex < 0 || support.PartIndex >= len(blocks) || blocks[support.PartIndex].Type != schema.ContentBlockTypeAssistantGenText || blocks[support.PartIndex].Text == nil {
			return fmt.Errorf("grounding support %d targets a non-text block", i)
		}
		text := *blocks[support.PartIndex].Text
		if err := validateTextRange(text, support.StartIndex, support.EndIndex); err != nil {
			return fmt.Errorf("grounding support %d: %w", i, err)
		}
		if support.Text != text[support.StartIndex:support.EndIndex] {
			return fmt.Errorf("grounding support %d text does not match its target range", i)
		}
	}
	return nil
}

func validatePublicProviderAnnotations(annotations *PublicProviderAnnotations, text string) error {
	if annotations == nil {
		return nil
	}
	limits := DefaultProjectionLimits()
	annotationCount := 0
	if err := addBoundedCount(&annotationCount, len(annotations.OpenAI), limits.MaxAnnotations, "provider annotation limit exceeded"); err != nil {
		return err
	}
	if err := addBoundedCount(&annotationCount, len(annotations.Claude), limits.MaxAnnotations, "provider annotation limit exceeded"); err != nil {
		return err
	}
	for i, annotation := range annotations.OpenAI {
		var err error
		switch openai.TextAnnotationType(annotation.Type) {
		case openai.TextAnnotationTypeFileCitation:
			if annotation.FileID == "" || annotation.Index < 0 || annotation.URL != "" || annotation.ContainerID != "" || annotation.StartIndex != 0 || annotation.EndIndex != 0 {
				err = errors.New("invalid file citation")
			}
		case openai.TextAnnotationTypeURLCitation:
			if annotation.URL == "" || annotation.FileID != "" || annotation.ContainerID != "" || annotation.Index != 0 {
				err = errors.New("invalid URL citation")
			} else {
				err = validateTextRange(text, annotation.StartIndex, annotation.EndIndex)
			}
		case openai.TextAnnotationTypeContainerFileCitation:
			if annotation.ContainerID == "" || annotation.FileID == "" || annotation.URL != "" || annotation.Index != 0 {
				err = errors.New("invalid container file citation")
			} else {
				err = validateTextRange(text, annotation.StartIndex, annotation.EndIndex)
			}
		case openai.TextAnnotationTypeFilePath:
			if annotation.FileID == "" || annotation.Index < 0 || annotation.URL != "" || annotation.ContainerID != "" || annotation.StartIndex != 0 || annotation.EndIndex != 0 {
				err = errors.New("invalid file path annotation")
			}
		default:
			err = errors.New("unknown OpenAI annotation type")
		}
		if err != nil {
			return fmt.Errorf("OpenAI annotation %d: %w", i, err)
		}
	}
	for i, citation := range annotations.Claude {
		var err error
		switch claude.TextCitationType(citation.Type) {
		case claude.TextCitationTypeCharLocation, claude.TextCitationTypePageLocation, claude.TextCitationTypeContentBlockLocation:
			if citation.DocumentIndex < 0 || citation.StartIndex < 0 || citation.EndIndex < citation.StartIndex || citation.URL != "" {
				err = errors.New("invalid source citation")
			}
		case claude.TextCitationTypeWebSearchResultLocation:
			if citation.URL == "" || citation.DocumentTitle != "" || citation.DocumentIndex != 0 || citation.StartIndex != 0 || citation.EndIndex != 0 {
				err = errors.New("invalid web citation")
			}
		default:
			err = errors.New("unknown Claude citation type")
		}
		if err != nil {
			return fmt.Errorf("claude citation %d: %w", i, err)
		}
	}
	if annotations.RefusalReason == "" && len(annotations.OpenAI) == 0 && len(annotations.Claude) == 0 {
		return errors.New("provider annotations have no public fields")
	}
	return nil
}

func validatePublicResponseMeta(meta *PublicResponseMeta) error {
	if meta == nil {
		return errors.New("response metadata is required")
	}
	if meta.TokenUsage != nil {
		if err := meta.TokenUsage.Validate(); err != nil {
			return err
		}
	}
	if grounding := meta.GeminiGrounding; grounding != nil {
		limits := DefaultProjectionLimits()
		annotationCount := 0
		if err := addBoundedCount(&annotationCount, len(grounding.Chunks), limits.MaxAnnotations, "grounding entry limit exceeded"); err != nil {
			return err
		}
		if err := addBoundedCount(&annotationCount, len(grounding.Supports), limits.MaxAnnotations, "grounding entry limit exceeded"); err != nil {
			return err
		}
		entries := 0
		if err := addBoundedCount(&entries, len(grounding.WebSearchQueries), limits.MaxJSONEntries, "grounding value entry limit exceeded"); err != nil {
			return err
		}
		for i, support := range grounding.Supports {
			if support.PartIndex < 0 || support.StartIndex < 0 || support.EndIndex < support.StartIndex {
				return fmt.Errorf("grounding support %d has invalid target range", i)
			}
			if err := addBoundedCount(&entries, len(support.GroundingChunkIndices), limits.MaxJSONEntries, "grounding value entry limit exceeded"); err != nil {
				return err
			}
			if err := addBoundedCount(&entries, len(support.ConfidenceScores), limits.MaxJSONEntries, "grounding value entry limit exceeded"); err != nil {
				return err
			}
			for _, index := range support.GroundingChunkIndices {
				if index < 0 || index >= len(grounding.Chunks) {
					return fmt.Errorf("grounding support %d has invalid chunk index", i)
				}
			}
			for _, score := range support.ConfidenceScores {
				if math.IsNaN(float64(score)) || math.IsInf(float64(score), 0) || score < 0 || score > 1 {
					return fmt.Errorf("grounding support %d has invalid confidence score", i)
				}
			}
		}
	}
	if meta.TokenUsage == nil && meta.OpenAIStatus == "" && meta.OpenAIError == nil && meta.OpenAIIncompleteReason == "" && meta.ClaudeStopReason == "" && meta.ClaudeStopSequence == "" && meta.ClaudeStopCategory == "" && meta.ClaudeStopExplanation == "" && meta.GeminiFinishReason == "" && meta.GeminiGrounding == nil {
		return errors.New("response metadata has no public fields")
	}
	return nil
}

func validatePublicToolDefinition(tool *PublicToolDefinition, limits ProjectionLimits) error {
	if tool == nil || tool.Name == "" {
		return errors.New("tool name is required")
	}
	switch tool.ParamsKind {
	case "none":
		if tool.Params != nil || len(tool.JSONSchema) != 0 {
			return errors.New("none parameters carry a representation")
		}
	case "params":
		if tool.Params == nil || len(tool.JSONSchema) != 0 {
			return errors.New("structured parameters are invalid")
		}
		entries := 0
		if err := addBoundedCount(&entries, len(tool.Params), limits.MaxJSONEntries, "structured parameter limit exceeded"); err != nil {
			return errors.New("structured parameter limit exceeded")
		}
		for name, parameter := range tool.Params {
			if name == "" || parameter == nil {
				return errors.New("structured parameter names and values are required")
			}
			if err := validatePublicParameter(parameter, 1, limits, &entries, map[*PublicParameterInfo]bool{}); err != nil {
				return fmt.Errorf("parameter %s: %w", name, err)
			}
		}
	case "json_schema":
		if tool.Params != nil || len(tool.JSONSchema) == 0 || !json.Valid(tool.JSONSchema) {
			return errors.New("JSON Schema parameters are invalid")
		}
	default:
		return errors.New("parameter representation is unknown")
	}
	return nil
}

func validatePublicParameter(parameter *PublicParameterInfo, depth int, limits ProjectionLimits, entries *int, stack map[*PublicParameterInfo]bool) error {
	if parameter == nil {
		return errors.New("parameter is nil")
	}
	if err := validateParameterShape(parameter.Type, parameter.ElemInfo != nil, parameter.SubParams != nil, len(parameter.Enum) != 0); err != nil {
		return err
	}
	if depth > limits.MaxJSONDepth {
		return errors.New("parameter depth limit exceeded")
	}
	if stack[parameter] {
		return errors.New("parameter contains a cycle")
	}
	if err := addBoundedCount(entries, len(parameter.SubParams), limits.MaxJSONEntries, "parameter entry limit exceeded"); err != nil {
		return err
	}
	if err := addBoundedCount(entries, len(parameter.Enum), limits.MaxJSONEntries, "parameter entry limit exceeded"); err != nil {
		return errors.New("parameter entry limit exceeded")
	}
	stack[parameter] = true
	defer delete(stack, parameter)
	if parameter.ElemInfo != nil {
		if err := validatePublicParameter(parameter.ElemInfo, depth+1, limits, entries, stack); err != nil {
			return err
		}
	}
	for name, child := range parameter.SubParams {
		if name == "" || child == nil {
			return errors.New("nested parameter names and values are required")
		}
		if err := validatePublicParameter(child, depth+1, limits, entries, stack); err != nil {
			return err
		}
	}
	return nil
}

func validateParameterShape(parameterType schema.DataType, hasElement, hasSubParams, hasEnum bool) error {
	switch parameterType {
	case schema.Object:
		if hasElement || hasEnum {
			return errors.New("object parameter has incompatible element or enum fields")
		}
	case schema.Array:
		if hasSubParams || hasEnum {
			return errors.New("array parameter has incompatible object or enum fields")
		}
	case schema.String:
		if hasElement || hasSubParams {
			return errors.New("string parameter has incompatible nested fields")
		}
	case schema.Number, schema.Integer, schema.Null, schema.Boolean:
		if hasElement || hasSubParams || hasEnum {
			return errors.New("scalar parameter has incompatible nested or enum fields")
		}
	default:
		return errors.New("parameter type is unknown")
	}
	return nil
}

func validatePublicMedia(media *PublicMedia) error {
	if media == nil || (media.URL == "") == (media.Base64Data == "") {
		return errors.New("media requires exactly one URL or base64 source")
	}
	if media.Base64Data != "" && media.MIMEType == "" {
		return errors.New("base64 media requires a MIME type")
	}
	return nil
}

func validatePublicFunctionResult(result *PublicFunctionToolResult) error {
	if result == nil || result.CallID == "" || result.Name == "" {
		return errors.New("function result requires call ID and name")
	}
	for _, part := range result.Content {
		switch part.Type {
		case schema.FunctionToolResultContentBlockTypeText:
			if part.Media != nil {
				return errors.New("text result part has media")
			}
		case schema.FunctionToolResultContentBlockTypeImage, schema.FunctionToolResultContentBlockTypeAudio,
			schema.FunctionToolResultContentBlockTypeVideo, schema.FunctionToolResultContentBlockTypeFile:
			if err := validatePublicMedia(part.Media); err != nil || part.Text != "" {
				return errors.New("media result part is invalid")
			}
		default:
			return errors.New("function result part type is unknown")
		}
	}
	return nil
}

func publicBlockCallID(block *PublicContentBlock) string {
	switch block.Type {
	case schema.ContentBlockTypeToolSearchResult:
		return block.ToolSearchResult.CallID
	case schema.ContentBlockTypeFunctionToolCall:
		return block.FunctionToolCall.CallID
	case schema.ContentBlockTypeFunctionToolResult:
		return block.FunctionToolResult.CallID
	case schema.ContentBlockTypeServerToolCall:
		return block.ServerToolCall.CallID
	case schema.ContentBlockTypeServerToolResult:
		return block.ServerToolResult.CallID
	case schema.ContentBlockTypeMCPToolCall:
		return block.MCPToolCall.CallID
	case schema.ContentBlockTypeMCPToolResult:
		return block.MCPToolResult.CallID
	default:
		return ""
	}
}

func validateInterruptTargets(targets []InterruptTargetV1) error {
	if len(targets) == 0 || len(targets) > DefaultProjectionLimits().MaxInterruptTargets {
		return errors.New("interrupt targets must be non-empty and within the configured limit")
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.ID == "" || target.Address == "" || seen[target.ID+"\x00"+target.Address] {
			return errors.New("interrupt target IDs and addresses must be non-empty and unique")
		}
		seen[target.ID+"\x00"+target.Address] = true
	}
	return nil
}

func validateCorrelation(correlation *ApprovalInterruptCorrelation, targets []InterruptTargetV1) error {
	if correlation == nil {
		return nil
	}
	if correlation.ApprovalRequestID == "" || correlation.InterruptTargetID == "" || correlation.InterruptAddress == "" {
		return errors.New("approval correlation fields are required and remain distinct")
	}
	for _, target := range targets {
		if target.ID == correlation.InterruptTargetID && target.Address == correlation.InterruptAddress {
			return nil
		}
	}
	return errors.New("approval correlation does not identify an interrupt target")
}

func ValidateCommitReceipt(receipt CommitReceiptV1, projection *PublicAgenticMessage, envelope *AgenticEnvelopeV1) error {
	if receipt.Revision == "" {
		return errors.New("commit receipt revision is required")
	}
	if projection != nil {
		if receipt.Domain != "projection" || receipt.Digest != projection.Digest || !reflect.DeepEqual(receipt.Identity, projection.Identity) {
			return errors.New("commit receipt does not bind projection")
		}
		want, err := ProjectionDigestV1(projection)
		if err != nil || want != receipt.Digest {
			return errors.New("commit receipt projection digest mismatch")
		}
		return nil
	}
	if envelope != nil {
		if receipt.Domain != "lifecycle" || receipt.Kind != envelope.Kind || receipt.Digest != envelope.Digest || !reflect.DeepEqual(receipt.Identity, envelope.Identity) {
			return errors.New("commit receipt does not bind lifecycle fact")
		}
		want, err := LifecycleDigestV1(envelope)
		if err != nil || want != receipt.Digest {
			return errors.New("commit receipt lifecycle digest mismatch")
		}
		return nil
	}
	return errors.New("commit receipt candidate is required")
}
