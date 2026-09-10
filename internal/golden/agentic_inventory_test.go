package golden

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

// These lists intentionally fail review when the pinned Eino inventory changes:
// every entry must also appear in convert's closed projection switch.
func TestAgenticContentKindInventory(t *testing.T) {
	kinds := []schema.ContentBlockType{
		schema.ContentBlockTypeReasoning,
		schema.ContentBlockTypeUserInputText,
		schema.ContentBlockTypeUserInputImage,
		schema.ContentBlockTypeUserInputAudio,
		schema.ContentBlockTypeUserInputVideo,
		schema.ContentBlockTypeUserInputFile,
		schema.ContentBlockTypeToolSearchResult,
		schema.ContentBlockTypeAssistantGenText,
		schema.ContentBlockTypeAssistantGenImage,
		schema.ContentBlockTypeAssistantGenAudio,
		schema.ContentBlockTypeAssistantGenVideo,
		schema.ContentBlockTypeFunctionToolCall,
		schema.ContentBlockTypeFunctionToolResult,
		schema.ContentBlockTypeServerToolCall,
		schema.ContentBlockTypeServerToolResult,
		schema.ContentBlockTypeMCPToolCall,
		schema.ContentBlockTypeMCPToolResult,
		schema.ContentBlockTypeMCPListToolsResult,
		schema.ContentBlockTypeMCPToolApprovalRequest,
		schema.ContentBlockTypeMCPToolApprovalResponse,
	}
	assertUniqueInventory(t, "content kind", 20, kinds)
}

func TestFunctionResultKindInventory(t *testing.T) {
	kinds := []schema.FunctionToolResultContentBlockType{
		schema.FunctionToolResultContentBlockTypeText,
		schema.FunctionToolResultContentBlockTypeImage,
		schema.FunctionToolResultContentBlockTypeAudio,
		schema.FunctionToolResultContentBlockTypeVideo,
		schema.FunctionToolResultContentBlockTypeFile,
	}
	assertUniqueInventory(t, "function result kind", 5, kinds)
}

func assertUniqueInventory[T comparable](t *testing.T, name string, want int, values []T) {
	t.Helper()
	if len(values) != want {
		t.Fatalf("%s inventory has %d entries, want %d", name, len(values), want)
	}
	seen := make(map[T]bool, len(values))
	for _, value := range values {
		if seen[value] {
			t.Fatalf("duplicate %s %v", name, value)
		}
		seen[value] = true
	}
}
