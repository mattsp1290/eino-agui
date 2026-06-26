package tools

import (
	"encoding/json"
	"fmt"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// ClientToolInfos converts AG-UI client tool definitions into eino ToolInfos.
func ClientToolInfos(tools []aguitypes.Tool) ([]*schema.ToolInfo, error) {
	out := make([]*schema.ToolInfo, 0, len(tools))
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool.Name == "" {
			return nil, fmt.Errorf("a client tool has an empty name")
		}
		if seen[tool.Name] {
			return nil, fmt.Errorf("duplicate client tool name %q", tool.Name)
		}
		seen[tool.Name] = true

		info := &schema.ToolInfo{Name: tool.Name, Desc: tool.Description}
		params, err := ToJSONSchema(tool.Parameters)
		if err != nil {
			return nil, fmt.Errorf("tool %q parameters: %w", tool.Name, err)
		}
		if params != nil {
			info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(params)
		}
		out = append(out, info)
	}
	return out, nil
}

// ToJSONSchema converts client-supplied JSON Schema data into eino's JSON
// schema type. Nil and JSON null mean the tool takes no arguments.
func ToJSONSchema(params any) (*jsonschema.Schema, error) {
	if params == nil {
		return nil, nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if string(data) == "null" {
		return nil, nil
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("not a valid JSON Schema: %w", err)
	}
	return &schema, nil
}

// ClassifyToolCalls splits actionable model tool calls into server-owned and
// client-defined calls.
func ClassifyToolCalls(calls []schema.ToolCall, clientNames map[string]bool) (server, client []schema.ToolCall) {
	for _, call := range calls {
		if call.ID == "" {
			continue
		}
		if clientNames[call.Function.Name] {
			client = append(client, call)
		} else {
			server = append(server, call)
		}
	}
	return server, client
}
