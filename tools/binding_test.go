package tools

import (
	"strings"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
)

func TestClientToolInfos(t *testing.T) {
	infos, err := ClientToolInfos([]aguitypes.Tool{{
		Name:        "lookup_weather",
		Description: "Read weather from the browser client",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string"},
			},
			"required": []any{"city"},
		},
	}})
	if err != nil {
		t.Fatalf("ClientToolInfos() error = %v", err)
	}
	if got, want := len(infos), 1; got != want {
		t.Fatalf("len(infos) = %d, want %d", got, want)
	}
	if got, want := infos[0].Name, "lookup_weather"; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := infos[0].Desc, "Read weather from the browser client"; got != want {
		t.Fatalf("Desc = %q, want %q", got, want)
	}
	if infos[0].ParamsOneOf == nil {
		t.Fatal("ParamsOneOf = nil, want JSON schema params")
	}
}

func TestClientToolInfosValidation(t *testing.T) {
	_, err := ClientToolInfos([]aguitypes.Tool{{Name: ""}})
	if err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("empty name error = %v, want empty name", err)
	}

	_, err = ClientToolInfos([]aguitypes.Tool{{Name: "dup"}, {Name: "dup"}})
	if err == nil || !strings.Contains(err.Error(), `duplicate client tool name "dup"`) {
		t.Fatalf("duplicate error = %v, want duplicate name", err)
	}
}

func TestToJSONSchemaNilAndNull(t *testing.T) {
	for _, value := range []any{nil, jsonNull{}} {
		schema, err := ToJSONSchema(value)
		if err != nil {
			t.Fatalf("ToJSONSchema(%T) error = %v", value, err)
		}
		if schema != nil {
			t.Fatalf("ToJSONSchema(%T) = %#v, want nil", value, schema)
		}
	}
}

func TestToJSONSchemaRejectsUnsupportedKeywords(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want string
	}{
		{
			name: "mixed supported and unsupported",
			in: map[string]any{
				"type":       "object",
				"x-provider": "metadata",
			},
			want: `unsupported JSON Schema keyword "x-provider"`,
		},
		{
			name: "unsupported only",
			in: map[string]any{
				"x-provider": "metadata",
			},
			want: `unsupported JSON Schema keyword "x-provider"`,
		},
		{
			name: "nested unsupported only",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"x-provider": "metadata",
					},
				},
			},
			want: `unsupported JSON Schema keyword "properties.city.x-provider"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ToJSONSchema(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ToJSONSchema() error = %v, want %s", err, tt.want)
			}
		})
	}
}

func TestClassifyToolCalls(t *testing.T) {
	server, client := ClassifyToolCalls([]schema.ToolCall{
		{ID: "call-client", Type: "function", Function: schema.FunctionCall{Name: "lookup_weather", Arguments: "{}"}},
		{ID: "call-server", Type: "function", Function: schema.FunctionCall{Name: "file_read", Arguments: "{}"}},
		{ID: "", Type: "function", Function: schema.FunctionCall{Name: "lookup_weather", Arguments: "{}"}},
	}, map[string]bool{"lookup_weather": true})

	if got, want := len(client), 1; got != want {
		t.Fatalf("len(client) = %d, want %d", got, want)
	}
	if got, want := client[0].ID, "call-client"; got != want {
		t.Fatalf("client[0].ID = %q, want %q", got, want)
	}
	if got, want := len(server), 1; got != want {
		t.Fatalf("len(server) = %d, want %d", got, want)
	}
	if got, want := server[0].ID, "call-server"; got != want {
		t.Fatalf("server[0].ID = %q, want %q", got, want)
	}
}

type jsonNull struct{}

func (jsonNull) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}
