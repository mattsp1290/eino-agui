package golden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeSSEMasksRuntimeFields(t *testing.T) {
	raw := []byte("id: TEXT_MESSAGE_START_12345\n" +
		"data: {\"type\":\"TEXT_MESSAGE_START\",\"timestamp\":12345,\"messageId\":\"golden-msg-000001\",\"role\":\"assistant\"}\n\n" +
		"data: {\"type\":\"TOOL_CALL_ARGS\",\"timestamp\":12346,\"toolCallId\":\"call-weather\",\"delta\":\"{}\"}\n\n")

	frames, err := NormalizeSSE(raw)
	if err != nil {
		t.Fatalf("NormalizeSSE() error = %v", err)
	}
	if got, want := frames[0].ID, FrameIDPlaceholder; got != want {
		t.Fatalf("frame id = %q, want %q", got, want)
	}
	if got, want := frames[0].Data["timestamp"], TimestampPlaceholder; got != want {
		t.Fatalf("timestamp = %q, want %q", got, want)
	}
	if got, want := frames[0].Data["messageId"], MessageIDPlaceholder; got != want {
		t.Fatalf("messageId = %q, want %q", got, want)
	}
	if got, want := frames[1].Data["toolCallId"], "call-weather"; got != want {
		t.Fatalf("toolCallId = %q, want %q", got, want)
	}
}

func TestFrameHelpers(t *testing.T) {
	frames := []Frame{
		{Data: map[string]any{"type": "TEXT_MESSAGE_START"}},
		{Data: map[string]any{"type": "TEXT_MESSAGE_CONTENT"}},
		{Data: map[string]any{"type": "TEXT_MESSAGE_CONTENT"}},
	}
	if got, want := FrameTypes(frames), []string{"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_CONTENT"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FrameTypes() = %#v, want %#v", got, want)
	}
	if got, want := CountType(frames, "TEXT_MESSAGE_CONTENT"), 2; got != want {
		t.Fatalf("CountType() = %d, want %d", got, want)
	}
}

func TestGoldenFixtureFilesAreNormalized(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "testdata", "golden", "*.normalized.json"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if got, want := len(matches), 4; got != want {
		t.Fatalf("fixture count = %d, want %d", got, want)
	}

	units := map[string]bool{}
	for _, path := range matches {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", path, err)
		}
		var fixture map[string]any
		if err := json.Unmarshal(content, &fixture); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", path, err)
		}
		unit, ok := fixture["unit"].(string)
		if !ok || unit == "" {
			t.Fatalf("%s missing unit", path)
		}
		units[unit] = true
		assertNoUnmaskedRuntimeValues(t, path, fixture)
	}
	for _, unit := range []string{"convert", "emitter", "streamTurn", "toolBinding"} {
		if !units[unit] {
			t.Fatalf("missing fixture unit %q", unit)
		}
	}
}

func TestGoldenFixtureContracts(t *testing.T) {
	stream := readFixture(t, "stream_turn.normalized.json")
	streamFrames := fixtureFrames(t, stream)
	streamTypes := FrameTypes(streamFrames)
	wantStreamTypes := []string{
		"REASONING_START",
		"REASONING_MESSAGE_START",
		"REASONING_MESSAGE_CONTENT",
		"REASONING_MESSAGE_END",
		"REASONING_END",
		"TEXT_MESSAGE_START",
		"TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_END",
		"REASONING_START",
		"REASONING_MESSAGE_START",
		"REASONING_MESSAGE_CONTENT",
		"REASONING_MESSAGE_END",
		"REASONING_END",
		"TOOL_CALL_START",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_END",
	}
	if !reflect.DeepEqual(streamTypes, wantStreamTypes) {
		t.Fatalf("stream fixture event types = %#v, want %#v", streamTypes, wantStreamTypes)
	}
	if got, want := CountType(streamFrames, "TOOL_CALL_START"), 1; got != want {
		t.Fatalf("TOOL_CALL_START count = %d, want %d", got, want)
	}
	if got, want := streamFrames[14].Data["delta"], "{\"city\":"; got != want {
		t.Fatalf("buffered first tool args delta = %q, want %q", got, want)
	}

	emitter := readFixture(t, "emitter.normalized.json")
	emitterInput := emitter["input"].(map[string]any)
	emitterInputMessages := emitterInput["messages"].([]any)
	if !inputCarriesEncryptedFields(emitterInputMessages) {
		t.Fatal("emitter fixture input does not carry encrypted fields")
	}
	emitterFrames := fixtureFrames(t, emitter)
	messages := emitterFrames[0].Data["messages"].([]any)
	for _, message := range messages {
		m := message.(map[string]any)
		if _, ok := m["encryptedValue"]; ok {
			t.Fatal("emitter fixture includes encryptedValue after scrub")
		}
		if _, ok := m["encryptedContent"]; ok {
			t.Fatal("emitter fixture includes encryptedContent after scrub")
		}
	}

	toolBinding := readFixture(t, "tool_binding.normalized.json")
	toolFrames := fixtureFrames(t, toolBinding)
	if got, want := FrameTypes(toolFrames), []string{"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "MESSAGES_SNAPSHOT", "RUN_FINISHED"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool fixture event types = %#v, want %#v", got, want)
	}
	snapshotMessages := toolFrames[3].Data["messages"].([]any)
	if got, want := len(snapshotMessages), 3; got != want {
		t.Fatalf("tool handback snapshot message count = %d, want %d", got, want)
	}
	serverResult := snapshotMessages[2].(map[string]any)
	if got, want := serverResult["toolCallId"], "call-server"; got != want {
		t.Fatalf("synthetic server result toolCallId = %q, want %q", got, want)
	}
	if got := serverResult["content"].(string); !strings.Contains(got, "not executed") {
		t.Fatalf("synthetic server result content = %q, want not-executed error", got)
	}
	finished := toolFrames[4].Data
	outcome := finished["outcome"].(map[string]any)
	if got, want := outcome["type"], "success"; got != want {
		t.Fatalf("RUN_FINISHED outcome = %q, want %q", got, want)
	}
}

func assertNoUnmaskedRuntimeValues(t *testing.T, path string, value any) {
	t.Helper()

	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "timestamp":
				if child != TimestampPlaceholder {
					t.Fatalf("%s has unmasked timestamp %v", path, child)
				}
			case "messageId":
				if s, ok := child.(string); ok && looksGeneratedMessageID(s) {
					t.Fatalf("%s has unmasked messageId %q", path, s)
				}
			case "id":
				if s, ok := child.(string); ok && looksGeneratedMessageID(s) {
					t.Fatalf("%s has unmasked generated id %q", path, s)
				}
			}
			assertNoUnmaskedRuntimeValues(t, path, child)
		}
	case []any:
		for _, child := range v {
			assertNoUnmaskedRuntimeValues(t, path, child)
		}
	}
}

func looksGeneratedMessageID(s string) bool {
	return strings.HasPrefix(s, "msg-") ||
		strings.HasPrefix(s, "golden-msg-") ||
		strings.HasPrefix(s, "fixture-msg-")
}

func inputCarriesEncryptedFields(messages []any) bool {
	for _, message := range messages {
		m := message.(map[string]any)
		if _, ok := m["encryptedValue"]; ok {
			return true
		}
		if _, ok := m["encryptedContent"]; ok {
			return true
		}
	}
	return false
}

func readFixture(t *testing.T, name string) map[string]any {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", name))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", name, err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", name, err)
	}
	return fixture
}

func fixtureFrames(t *testing.T, fixture map[string]any) []Frame {
	t.Helper()

	rawFrames, ok := fixture["frames"].([]any)
	if !ok {
		t.Fatalf("fixture %q has no frames", fixture["name"])
	}
	frames := make([]Frame, len(rawFrames))
	for i, raw := range rawFrames {
		frame := raw.(map[string]any)
		data := frame["data"].(map[string]any)
		id, _ := frame["id"].(string)
		event, _ := frame["event"].(string)
		frames[i] = Frame{ID: id, Event: event, Data: data}
	}
	return frames
}
