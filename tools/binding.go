package tools

import (
	"encoding/json"
	"fmt"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// SchemaOption configures JSON Schema conversion for client tool binding.
type SchemaOption func(*schemaConfig)

type schemaConfig struct {
	allowUnsupportedKeywords bool
}

// WithUnsupportedSchemaKeywords allows JSON Schema keywords that eino's
// jsonschema package does not model. Unsupported keywords are dropped by the
// conversion, matching callers that prefer best-effort tool binding over
// rejecting a client tool definition.
func WithUnsupportedSchemaKeywords() SchemaOption {
	return func(cfg *schemaConfig) {
		cfg.allowUnsupportedKeywords = true
	}
}

// ClientToolInfos converts AG-UI client tool definitions into eino ToolInfos.
func ClientToolInfos(tools []aguitypes.Tool, opts ...SchemaOption) ([]*schema.ToolInfo, error) {
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
		params, err := ToJSONSchema(tool.Parameters, opts...)
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
func ToJSONSchema(params any, opts ...SchemaOption) (*jsonschema.Schema, error) {
	if params == nil {
		return nil, nil
	}
	cfg := schemaConfig{}
	for _, opt := range opts {
		opt(&cfg)
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
	if !cfg.allowUnsupportedKeywords {
		if err := rejectUnsupportedSchemaKeywords(data, &schema); err != nil {
			return nil, err
		}
	}
	return &schema, nil
}

func rejectUnsupportedSchemaKeywords(original []byte, parsed *jsonschema.Schema) error {
	roundTrip, err := json.Marshal(parsed)
	if err != nil {
		return err
	}

	var originalValue any
	if err := json.Unmarshal(original, &originalValue); err != nil {
		return err
	}
	var roundTripValue any
	if err := json.Unmarshal(roundTrip, &roundTripValue); err != nil {
		return err
	}
	return rejectUnsupportedValue("", originalValue, roundTripValue)
}

func rejectUnsupportedValue(path string, original, roundTrip any) error {
	originalMap, originalIsMap := original.(map[string]any)
	roundTripMap, roundTripIsMap := roundTrip.(map[string]any)
	if originalIsMap && roundTripIsMap {
		for key, originalChild := range originalMap {
			roundTripChild, ok := roundTripMap[key]
			if !ok {
				return fmt.Errorf("unsupported JSON Schema keyword %q", joinPath(path, key))
			}
			if err := rejectUnsupportedValue(joinPath(path, key), originalChild, roundTripChild); err != nil {
				return err
			}
		}
		return nil
	}
	if originalIsMap && !roundTripIsMap && len(originalMap) > 0 {
		for key := range originalMap {
			return fmt.Errorf("unsupported JSON Schema keyword %q", joinPath(path, key))
		}
	}

	originalList, originalIsList := original.([]any)
	roundTripList, roundTripIsList := roundTrip.([]any)
	if originalIsList && roundTripIsList {
		for i := range originalList {
			if i >= len(roundTripList) {
				return fmt.Errorf("unsupported JSON Schema element %q", joinPath(path, fmt.Sprintf("%d", i)))
			}
			if err := rejectUnsupportedValue(joinPath(path, fmt.Sprintf("%d", i)), originalList[i], roundTripList[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
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
