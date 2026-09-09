package convert

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestToAGUITokenUsage(t *testing.T) {
	usage, err := ToAGUITokenUsage(&schema.TokenUsage{
		PromptTokens:     11,
		CompletionTokens: 7,
		TotalTokens:      18,
		PromptTokenDetails: schema.PromptTokenDetails{
			CachedTokens: 3,
		},
		CompletionTokensDetails: schema.CompletionTokensDetails{
			ReasoningTokens: 2,
		},
	}, "openai", "gpt-test")
	if err != nil {
		t.Fatalf("ToAGUITokenUsage: %v", err)
	}
	if usage.Provider != "openai" || usage.Model != "gpt-test" {
		t.Fatalf("labels = %q/%q", usage.Provider, usage.Model)
	}
	assertCount(t, "input", usage.InputTokens, 11)
	assertCount(t, "output", usage.OutputTokens, 7)
	assertCount(t, "total", usage.TotalTokens, 18)
	assertCount(t, "reasoning", usage.ReasoningTokens, 2)
	assertCount(t, "cached", usage.CachedInputTokens, 3)
}

func TestToAGUITokenUsageAbsentZeroPartialAndNegative(t *testing.T) {
	for _, input := range []*schema.TokenUsage{nil, {}} {
		got, err := ToAGUITokenUsage(input, "", "")
		if err != nil || got != nil {
			t.Fatalf("ToAGUITokenUsage(%#v) = %#v, %v; want nil,nil", input, got, err)
		}
	}
	partial, err := ToAGUITokenUsage(&schema.TokenUsage{CompletionTokens: 4}, "p", "m")
	if err != nil || partial == nil || partial.OutputTokens == nil || *partial.OutputTokens != 4 {
		t.Fatalf("partial usage = %#v, %v", partial, err)
	}
	if partial.InputTokens != nil || partial.TotalTokens != nil {
		t.Fatalf("partial usage fabricated counts: %#v", partial)
	}
	if got, err := ToAGUITokenUsage(&schema.TokenUsage{PromptTokens: -1}, "", ""); err == nil || got != nil {
		t.Fatalf("negative usage = %#v, %v; want nil,error", got, err)
	}
}

func assertCount(t *testing.T, name string, got *int64, want int64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s count = %v, want %d", name, got, want)
	}
}
