#!/usr/bin/env bash
set -euo pipefail

repo="${REFERENCE_APP_DIR:-/Users/punk1290/git/ag-ui-go-server-example}"
expected_commit="a6dd6fd896ead9a06014a8a4bed0bb6a1a6cdfb5"

if [[ ! -d "$repo/.git" ]]; then
  echo "reference app checkout not found: $repo" >&2
  exit 1
fi

actual_commit="$(git -C "$repo" rev-parse HEAD)"
if [[ "$actual_commit" != "$expected_commit" ]]; then
  echo "reference app commit mismatch: got $actual_commit, want $expected_commit" >&2
  exit 1
fi

tmp_test="$repo/internal/agent/golden_capture_external_test.go"
cleanup() {
  rm -f "$tmp_test"
}
trap cleanup EXIT

cat > "$tmp_test" <<'GOEOF'
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestGoldenCaptureStreamTurnOrdering(t *testing.T) {
	idx := 0
	fm := captureModel{chunks: []*schema.Message{
		{Role: schema.Assistant, ReasoningContent: "think "},
		{Role: schema.Assistant, Content: "answer "},
		{Role: schema.Assistant, ReasoningContent: "again"},
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Index: &idx, Type: "function", Function: schema.FunctionCall{Arguments: "{\"city\":"}}}},
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{Index: &idx, ID: "call-weather", Type: "function", Function: schema.FunctionCall{Name: "get_weather", Arguments: "\"NYC\"}"}}}},
	}}

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	emit := NewEmitter(context.Background(), w, sse.NewSSEWriter(), "thread-golden", "run-golden", nil)
	if _, err := streamTurn(context.Background(), emit, &fm, nil, true); err != nil {
		t.Fatalf("streamTurn: %v", err)
	}
	_ = w.Flush()
	frames := parseFrames(t, buf.String())
	got := frameTypes(frames)
	want := []string{
		"REASONING_START", "REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT", "REASONING_MESSAGE_END", "REASONING_END",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
		"REASONING_START", "REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT", "REASONING_MESSAGE_END", "REASONING_END",
		"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_ARGS", "TOOL_CALL_END",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %#v, want %#v", got, want)
	}
	if frames[14]["delta"] != "{\"city\":" {
		t.Fatalf("buffered args delta = %#v", frames[14]["delta"])
	}
}

func TestGoldenCaptureEmitterScrub(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	emit := NewEmitter(context.Background(), w, sse.NewSSEWriter(), "thread-golden", "run-golden", nil)
	emit.MessagesSnapshot([]aguitypes.Message{{
		ID: "msg-input-1", Role: aguitypes.RoleAssistant, Content: "visible answer",
		EncryptedValue: "cipher-value", EncryptedContent: "cipher-content",
	}})
	_ = w.Flush()
	frames := parseFrames(t, buf.String())
	messages := frames[0]["messages"].([]any)
	message := messages[0].(map[string]any)
	if _, ok := message["encryptedValue"]; ok {
		t.Fatal("encryptedValue was not scrubbed")
	}
	if _, ok := message["encryptedContent"]; ok {
		t.Fatal("encryptedContent was not scrubbed")
	}
}

func TestGoldenCaptureConvertVisionGate(t *testing.T) {
	msg := aguitypes.Message{Role: aguitypes.RoleUser, Content: []aguitypes.InputContent{
		{Type: aguitypes.InputContentTypeText, Text: "First line"},
		{Type: aguitypes.InputContentTypeImage, Source: &aguitypes.InputContentSource{Type: aguitypes.InputContentSourceTypeURL, Value: "https://example.test/cat.png"}},
		{Type: aguitypes.InputContentTypeText, Text: "Second line"},
	}}
	openai := toEinoMessages([]aguitypes.Message{msg}, "openai")
	if len(openai) != 1 || len(openai[0].UserInputMultiContent) != 3 {
		t.Fatalf("openai multimodal content = %#v", openai)
	}
	codex := toEinoMessages([]aguitypes.Message{msg}, "openai-codex")
	if len(codex) != 1 || codex[0].Content != "First line\nSecond line" || len(codex[0].UserInputMultiContent) != 0 {
		t.Fatalf("codex text-only content = %#v", codex)
	}
}

func TestGoldenCaptureToolBindingHandbackSnapshot(t *testing.T) {
	server, client := classifyToolCalls([]schema.ToolCall{
		{ID: "call-client", Type: "function", Function: schema.FunctionCall{Name: "lookup_weather", Arguments: "{\"city\":\"NYC\"}"}},
		{ID: "call-server", Type: "function", Function: schema.FunctionCall{Name: "file_read", Arguments: "{\"path\":\"README.md\"}"}},
	}, map[string]bool{"lookup_weather": true})
	if len(client) != 1 || client[0].ID != "call-client" || len(server) != 1 || server[0].ID != "call-server" {
		t.Fatalf("classification server=%#v client=%#v", server, client)
	}
}

type captureModel struct{ chunks []*schema.Message }

func (m *captureModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.ConcatMessages(m.chunks)
}

func (m *captureModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray(m.chunks), nil
}

func (m *captureModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) { return m, nil }

func parseFrames(t *testing.T, out string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, frame := range strings.Split(out, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var data map[string]any
		for _, line := range strings.Split(frame, "\n") {
			if strings.HasPrefix(line, "data: ") {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
					t.Fatalf("decode frame %q: %v", frame, err)
				}
			}
		}
		if data == nil {
			t.Fatalf("frame missing data: %q", frame)
		}
		frames = append(frames, data)
	}
	return frames
}

func frameTypes(frames []map[string]any) []string {
	out := make([]string, len(frames))
	for i := range frames {
		out[i] = frames[i]["type"].(string)
	}
	return out
}

var _ = errors.Is
var _ = io.EOF
var _ = aguievents.EventTypeRunStarted
GOEOF

(cd "$repo" && go test ./internal/agent -run 'TestGoldenCapture' -count=1)
