package tools

import (
	"encoding/json"
	"os"
	"reflect"
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

func TestToolBindingMatchesNormalizedGoldenFixture(t *testing.T) {
	fixture := readToolBindingFixture(t)

	infos, err := ClientToolInfos(fixture.Input.ClientTools)
	if err != nil {
		t.Fatalf("ClientToolInfos() error = %v", err)
	}
	gotInfos := make([]goldenToolInfo, 0, len(infos))
	for _, info := range infos {
		gotInfos = append(gotInfos, goldenToolInfo{
			Name:           info.Name,
			Description:    info.Desc,
			HasParamsOneOf: info.ParamsOneOf != nil,
		})
	}
	if !reflect.DeepEqual(gotInfos, fixture.Normalized.ClientToolInfos) {
		t.Fatalf("client tool infos = %#v, want %#v", gotInfos, fixture.Normalized.ClientToolInfos)
	}

	calls := make([]schema.ToolCall, 0, len(fixture.Input.ActionableToolCalls))
	for _, call := range fixture.Input.ActionableToolCalls {
		calls = append(calls, schema.ToolCall{
			ID:   call.ID,
			Type: "function",
			Function: schema.FunctionCall{
				Name:      call.Name,
				Arguments: call.Arguments,
			},
		})
	}
	server, client := ClassifyToolCalls(calls, map[string]bool{"lookup_weather": true})
	if got := toolCallIDs(client); !reflect.DeepEqual(got, fixture.Normalized.Classified.Client) {
		t.Fatalf("client IDs = %v, want %v", got, fixture.Normalized.Classified.Client)
	}
	if got := toolCallIDs(server); !reflect.DeepEqual(got, fixture.Normalized.Classified.Server) {
		t.Fatalf("server IDs = %v, want %v", got, fixture.Normalized.Classified.Server)
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

func TestToJSONSchemaMalformedInputDoesNotPanic(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "not marshalable",
			in:   map[string]any{"type": func() {}},
			want: "unsupported type: func()",
		},
		{
			name: "not a schema shape",
			in:   []any{"not", "an", "object"},
			want: "not a valid JSON Schema",
		},
		{
			name: "lossy nested array element",
			in: map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"x-provider": "metadata"},
				},
			},
			want: `unsupported JSON Schema keyword "oneOf.1.x-provider"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ToJSONSchema() panicked: %v", r)
				}
			}()
			_, err := ToJSONSchema(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ToJSONSchema() error = %v, want containing %q", err, tt.want)
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

func TestToolsPackageDoesNotMentionRouteTypes(t *testing.T) {
	for _, path := range []string{"binding.go", "doc.go"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, forbidden := range []string{"RunConfig", "ToolPolicy", "runstore", "gofiber", "fasthttp"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("%s contains route/app type marker %q", path, forbidden)
			}
		}
	}
}

type jsonNull struct{}

func (jsonNull) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}

func readToolBindingFixture(t *testing.T) toolBindingFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/golden/tool_binding.normalized.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fixture toolBindingFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}
	return fixture
}

func toolCallIDs(calls []schema.ToolCall) []string {
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		ids = append(ids, call.ID)
	}
	return ids
}

type toolBindingFixture struct {
	Input struct {
		ClientTools         []aguitypes.Tool `json:"clientTools"`
		ActionableToolCalls []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"actionableToolCalls"`
	} `json:"input"`
	Normalized struct {
		ClientToolInfos []goldenToolInfo `json:"clientToolInfos"`
		Classified      struct {
			Client []string `json:"client"`
			Server []string `json:"server"`
		} `json:"classified"`
	} `json:"normalized"`
}

type goldenToolInfo struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	HasParamsOneOf bool   `json:"hasParamsOneOf"`
}
