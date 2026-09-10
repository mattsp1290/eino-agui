package golden

import "testing"

func TestTargetFeatureInventory(t *testing.T) {
	type feature struct {
		name        string
		disposition string
		test        string
	}
	features := []feature{
		{"text/reasoning events", "typed-helper", "TestEmitterEventFamilies"},
		{"state/activity/custom events", "typed-helper", "TestEmitterEventFamilies"},
		{"run success/interrupt/error usage", "typed-helper", "TestStreamUsageMapsToRunEndings"},
		{"subagent lifecycle", "typed-helper", "TestEmitterTargetProtocolFieldsAndGenericPath"},
		{"event metadata and run parent/input", "generic-emit", "TestEmitterTargetProtocolFieldsAndGenericPath"},
		{"message metadata", "bridge-round-trip", "TestMessageAndToolCallProtocolEnvelopeRoundTrip"},
		{"message subagentRunId", "bridge-round-trip", "TestMessageAndToolCallProtocolEnvelopeRoundTrip"},
		{"tool-call metadata", "bridge-round-trip", "TestMessageAndToolCallProtocolEnvelopeRoundTrip"},
		{"tool-call encryptedValue", "bridge-round-trip", "TestMessageAndToolCallProtocolEnvelopeRoundTrip"},
		{"tool-definition metadata", "one-way-binding", "TestClientToolInfosPreservesMetadataWithoutAliasing"},
		{"tool parentMessageId", "typed-helper", "TestStreamTurnEmitsReasoningTextAndLiveToolCalls"},
		{"capability models", "direct-sdk", "TestUpgradeAPIContracts"},
		{"AgenticModel stream", "typed-helper", "TestStreamAgenticTurnCorrelatesIndexedChunks"},
		{"typed ADK lifecycle", "typed-helper", "TestDrainAgenticEventsCompleteAndStreamingMessages"},
		{"agentic tool search", "bridge-round-trip", "TestProjectAgenticMessageAllContentKinds"},
		{"HTTP/persistence/tool execution", "out-of-scope", ""},
		{"agentic audio/video/document/binary projection", "bridge-round-trip", "TestProjectAgenticMessageAllContentKinds"},
	}
	allowed := map[string]bool{
		"typed-helper": true, "bridge-round-trip": true, "one-way-binding": true,
		"generic-emit": true, "direct-sdk": true, "out-of-scope": true,
	}
	seen := make(map[string]bool, len(features))
	for _, feature := range features {
		if feature.name == "" || seen[feature.name] {
			t.Fatalf("missing or duplicate feature name %q", feature.name)
		}
		seen[feature.name] = true
		if !allowed[feature.disposition] {
			t.Fatalf("feature %q has unknown disposition %q", feature.name, feature.disposition)
		}
		if feature.disposition != "out-of-scope" && feature.test == "" {
			t.Fatalf("supported feature %q has no linked test", feature.name)
		}
	}
}
