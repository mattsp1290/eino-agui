package testmodel

import "github.com/cloudwego/eino/schema"

// MixedStreamChunks returns a representative stream covering reasoning, text,
// encrypted reasoning signature metadata, and a streamed tool call.
func MixedStreamChunks() []*schema.Message {
	chunks := []*schema.Message{
		ReasoningChunk("think "),
		EncryptedReasoningChunk("hidden", "sig-fixture"),
		TextChunk("Hello "),
		TextChunk("world"),
	}
	chunks = append(chunks, ToolCallChunks(0, "call-weather", "get_weather", `{"city":`, `"NYC"}`)...)
	return chunks
}

// TextChunk returns an assistant text delta.
func TextChunk(content string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, Content: content}
}

// ReasoningChunk returns an assistant plaintext reasoning delta.
func ReasoningChunk(content string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, ReasoningContent: content}
}

// EncryptedReasoningChunk returns a reasoning output part carrying a signature.
func EncryptedReasoningChunk(text, signature string) *schema.Message {
	return &schema.Message{
		Role: schema.Assistant,
		AssistantGenMultiContent: []schema.MessageOutputPart{{
			Type: schema.ChatMessagePartTypeReasoning,
			Reasoning: &schema.MessageOutputReasoning{
				Text:      text,
				Signature: signature,
			},
			StreamingMeta: &schema.MessageStreamingMeta{Index: 0},
		}},
	}
}

// ToolCallChunks returns streaming tool-call chunks that share a stable Index
// pointer for the same call.
func ToolCallChunks(index int, id, name string, argumentFragments ...string) []*schema.Message {
	stableIndex := index
	chunks := make([]*schema.Message, 0, max(1, len(argumentFragments)))
	if len(argumentFragments) == 0 {
		return []*schema.Message{toolCallChunk(&stableIndex, id, name, "")}
	}
	for i, fragment := range argumentFragments {
		chunkID, chunkName := "", ""
		if i == 0 {
			chunkID = id
			chunkName = name
		}
		chunks = append(chunks, toolCallChunk(&stableIndex, chunkID, chunkName, fragment))
	}
	return chunks
}

func toolCallChunk(index *int, id, name, arguments string) *schema.Message {
	return &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			Index: index,
			ID:    id,
			Type:  "function",
			Function: schema.FunctionCall{
				Name:      name,
				Arguments: arguments,
			},
		}},
	}
}
