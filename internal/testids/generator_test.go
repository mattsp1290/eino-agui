package testids

import (
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

func TestGeneratorMonotonicIDs(t *testing.T) {
	generator := NewGenerator("")

	got := []string{
		generator.GenerateMessageID(),
		generator.GenerateToolCallID(),
		generator.GenerateRunID(),
		generator.GenerateThreadID(),
		generator.GenerateStepID(),
	}
	want := []string{
		"msg-000001",
		"tool-000002",
		"run-000003",
		"thread-000004",
		"step-000005",
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("id %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGeneratorPrefix(t *testing.T) {
	generator := NewGenerator("fixture")

	if got, want := generator.GenerateMessageID(), "fixture-msg-000001"; got != want {
		t.Fatalf("GenerateMessageID() = %q, want %q", got, want)
	}
}

func TestWithDeterministicGeneratorInstallsAndRestoresGlobal(t *testing.T) {
	original := aguievents.GetDefaultIDGenerator()

	t.Run("scoped global override", func(t *testing.T) {
		generator := WithDeterministicGenerator(t, "golden")

		if got := aguievents.GetDefaultIDGenerator(); got != generator {
			t.Fatalf("GetDefaultIDGenerator() = %T, want installed deterministic generator", got)
		}
		if got, want := aguievents.GenerateMessageID(), "golden-msg-000001"; got != want {
			t.Fatalf("GenerateMessageID() = %q, want %q", got, want)
		}
		if got, want := aguievents.GenerateToolCallID(), "golden-tool-000002"; got != want {
			t.Fatalf("GenerateToolCallID() = %q, want %q", got, want)
		}
	})

	if got := aguievents.GetDefaultIDGenerator(); got != original {
		t.Fatalf("GetDefaultIDGenerator() after cleanup = %T, want original %T", got, original)
	}
}

func TestWithGeneratorRejectsNil(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("WithGenerator did not panic for nil generator")
		}
	}()

	WithGenerator(t, nil)
}
