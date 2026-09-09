package convert

import (
	"fmt"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/schema"
)

// ToAGUITokenUsage converts Eino's classic chat-model usage into AG-UI's
// numeric per-model summary. Zero values are omitted because Eino cannot
// distinguish an absent count from an explicitly reported zero.
func ToAGUITokenUsage(usage *schema.TokenUsage, provider, model string) (*events.TokenUsage, error) {
	if usage == nil {
		return nil, nil
	}
	counts := []struct {
		name  string
		value int
	}{
		{"prompt tokens", usage.PromptTokens},
		{"completion tokens", usage.CompletionTokens},
		{"total tokens", usage.TotalTokens},
		{"reasoning tokens", usage.CompletionTokensDetails.ReasoningTokens},
		{"cached prompt tokens", usage.PromptTokenDetails.CachedTokens},
	}
	for _, count := range counts {
		if count.value < 0 {
			return nil, fmt.Errorf("invalid Eino usage: %s must not be negative", count.name)
		}
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 &&
		usage.CompletionTokensDetails.ReasoningTokens == 0 && usage.PromptTokenDetails.CachedTokens == 0 {
		return nil, nil
	}

	out := &events.TokenUsage{Provider: provider, Model: model}
	out.InputTokens = positiveTokenCount(usage.PromptTokens)
	out.OutputTokens = positiveTokenCount(usage.CompletionTokens)
	out.TotalTokens = positiveTokenCount(usage.TotalTokens)
	out.ReasoningTokens = positiveTokenCount(usage.CompletionTokensDetails.ReasoningTokens)
	out.CachedInputTokens = positiveTokenCount(usage.PromptTokenDetails.CachedTokens)
	return out, nil
}

func positiveTokenCount(count int) *int64 {
	if count <= 0 {
		return nil
	}
	value := int64(count)
	return &value
}
