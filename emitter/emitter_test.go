package emitter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/mattsp1290/eino-agui/internal/golden"
	"github.com/mattsp1290/eino-agui/internal/testsse"
)

func TestNewEmitterWritesLifecycleEvents(t *testing.T) {
	sink := testsse.NewSink()
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)

	emit.RunStarted()
	emit.RunFinishedSuccess()

	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{"RUN_STARTED", "RUN_FINISHED"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
	if got := frames[0].Data["threadId"]; got != "thread-1" {
		t.Fatalf("RUN_STARTED threadId = %v, want thread-1", got)
	}
	if got := frames[0].Data["runId"]; got != "run-1" {
		t.Fatalf("RUN_STARTED runId = %v, want run-1", got)
	}
	if got := frames[1].Data["runId"]; got != "run-1" {
		t.Fatalf("RUN_FINISHED runId = %v, want run-1", got)
	}
}

func TestEmitterEventFamilies(t *testing.T) {
	tests := []struct {
		name string
		emit func(*Emitter)
		want []string
	}{
		{
			name: "steps",
			emit: func(e *Emitter) {
				e.StepStarted("llm")
				e.StepFinished("llm")
			},
			want: []string{"STEP_STARTED", "STEP_FINISHED"},
		},
		{
			name: "text",
			emit: func(e *Emitter) {
				e.TextStart("msg-1")
				e.TextContent("msg-1", "hello")
				e.TextEnd("msg-1")
			},
			want: []string{"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END"},
		},
		{
			name: "reasoning",
			emit: func(e *Emitter) {
				e.ReasoningStart("reason-1")
				e.ReasoningMessageStart("reason-1")
				e.ReasoningContent("reason-1", "thinking")
				e.ReasoningMessageEnd("reason-1")
				e.ReasoningEnd("reason-1")
				e.ReasoningEncryptedValue(events.ReasoningEncryptedValueSubtypeMessage, "reason-1", "cipher")
			},
			want: []string{
				"REASONING_START",
				"REASONING_MESSAGE_START",
				"REASONING_MESSAGE_CONTENT",
				"REASONING_MESSAGE_END",
				"REASONING_END",
				"REASONING_ENCRYPTED_VALUE",
			},
		},
		{
			name: "state activity custom",
			emit: func(e *Emitter) {
				e.StateSnapshot(map[string]any{"status": "working"})
				e.StateDelta([]events.JSONPatchOperation{{Op: "replace", Path: "/status", Value: "done"}})
				e.ActivitySnapshot("activity-1", "approval", map[string]any{"text": "approve?"})
				e.ActivityDelta("activity-1", "approval", []events.JSONPatchOperation{{Op: "add", Path: "/ok", Value: true}})
				e.Custom("agent_complete", map[string]any{"ok": true})
			},
			want: []string{
				"STATE_SNAPSHOT",
				"STATE_DELTA",
				"ACTIVITY_SNAPSHOT",
				"ACTIVITY_DELTA",
				"CUSTOM",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := testsse.NewSink()
			emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
			tt.emit(emit)

			frames := normalizedFrames(t, sink)
			if got := golden.FrameTypes(frames); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("frame types = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmitterSkipsEmptyDeltasAndNormalizesEmptyToolResult(t *testing.T) {
	sink := testsse.NewSink()
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)

	emit.TextContent("msg-1", "")
	emit.ReasoningContent("reason-1", "")
	emit.ToolArgs("tool-1", "")
	emit.ToolStart("", "read_file")
	emit.ToolStart("tool-1", "")
	emit.ToolEnd("")
	emit.StateDelta(nil)
	emit.ActivityDelta("activity-1", "thinking", nil)
	emit.ToolResult("tool-msg-1", "tool-1", "")

	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{"TOOL_CALL_RESULT"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
	if got := frames[0].Data["content"]; got != "(empty)" {
		t.Fatalf("TOOL_CALL_RESULT content = %v, want (empty)", got)
	}
}

func TestToolEventsCloseOpenTextAndReasoningBlocks(t *testing.T) {
	sink := testsse.NewSink()
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)

	emit.TextStart("msg-1")
	emit.TextContent("msg-1", "partial")
	emit.ToolStart("tool-1", "file_read")

	emit.ReasoningMessageStart("reason-1")
	emit.ReasoningContent("reason-1", "thinking")
	emit.ToolResult("tool-msg-1", "tool-1", "done")

	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{
		"TEXT_MESSAGE_START",
		"TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_END",
		"TOOL_CALL_START",
		"REASONING_MESSAGE_START",
		"REASONING_MESSAGE_CONTENT",
		"REASONING_MESSAGE_END",
		"TOOL_CALL_RESULT",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
}

func TestDirectToolStartEmitsOnceAndRequiresStartedCallForArgsAndEnd(t *testing.T) {
	sink := testsse.NewSink()
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)

	emit.ToolArgs("tool-1", "before")
	emit.ToolEnd("tool-1")
	emit.ToolStart("tool-1", "file_read")
	emit.ToolStart("tool-1", "file_read")
	emit.ToolArgs("tool-1", "{}")
	emit.ToolEnd("tool-1")
	emit.ToolArgs("tool-1", "after")
	emit.ToolEnd("tool-1")
	emit.ToolStart("tool-1", "file_read")

	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{
		"TOOL_CALL_START",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_END",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
}

func TestMessagesSnapshotScrubsEncryptedValuesWithoutMutatingInput(t *testing.T) {
	sink := testsse.NewSink()
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	messages := []types.Message{
		{
			ID:               "msg-1",
			Role:             types.RoleAssistant,
			Content:          "visible answer",
			EncryptedValue:   "cipher-value",
			EncryptedContent: "cipher-content",
		},
	}

	emit.MessagesSnapshot(messages)

	if messages[0].EncryptedValue == "" || messages[0].EncryptedContent == "" {
		t.Fatal("MessagesSnapshot mutated input encrypted fields")
	}
	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{"MESSAGES_SNAPSHOT"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
	frameMessages, ok := frames[0].Data["messages"].([]any)
	if !ok || len(frameMessages) != 1 {
		t.Fatalf("messages payload = %#v, want one message", frames[0].Data["messages"])
	}
	frameMessage, ok := frameMessages[0].(map[string]any)
	if !ok {
		t.Fatalf("message payload = %#v, want object", frameMessages[0])
	}
	if _, ok := frameMessage["encryptedValue"]; ok {
		t.Fatalf("encryptedValue leaked in frame: %#v", frameMessage)
	}
	if _, ok := frameMessage["encryptedContent"]; ok {
		t.Fatalf("encryptedContent leaked in frame: %#v", frameMessage)
	}
}

func TestMessagesSnapshotMatchesNormalizedGoldenFixture(t *testing.T) {
	fixture := readEmitterFixture(t)
	sink := testsse.NewSink()
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)

	emit.MessagesSnapshot(fixture.Input.Messages)

	frames := normalizedFrames(t, sink)
	if got, want := comparableFrameData(frames), fixtureFrameData(fixture.Frames); !reflect.DeepEqual(got, want) {
		t.Fatalf("frame data = %#v, want %#v", got, want)
	}
}

func TestTransportErrorCancelsAndStopsSubsequentWrites(t *testing.T) {
	ctx := context.Background()
	writer := bufio.NewWriter(errorWriter{})
	var cancelCalls int
	emit := NewEmitter(ctx, writer, sse.NewSSEWriter(), "thread-1", "run-1", func() {
		cancelCalls++
	})

	emit.RunStarted()
	emit.RunFinishedSuccess()

	if emit.Err() == nil {
		t.Fatal("Err() is nil, want transport error")
	}
	if !strings.HasPrefix(emit.Err().Error(), "SSE flush failed:") {
		t.Fatalf("Err() = %q, want SSE flush failed prefix", emit.Err())
	}
	if cancelCalls != 1 {
		t.Fatalf("cancel calls = %d, want 1", cancelCalls)
	}
	if emit.EncErr() != nil {
		t.Fatalf("EncErr() = %v, want nil", emit.EncErr())
	}
}

func TestTransportWriteErrorCancelsThroughSDKWritePrefix(t *testing.T) {
	ctx := context.Background()
	writer := bufio.NewWriterSize(errorWriter{}, 1)
	var cancelCalls int
	emit := NewEmitter(ctx, writer, sse.NewSSEWriter(), "thread-1", "run-1", func() {
		cancelCalls++
	})

	emit.Custom("large-event", strings.Repeat("x", 1024))
	emit.RunFinishedSuccess()

	if emit.Err() == nil {
		t.Fatal("Err() is nil, want transport error")
	}
	if !strings.HasPrefix(emit.Err().Error(), "SSE write failed:") {
		t.Fatalf("Err() = %q, want SSE write failed prefix", emit.Err())
	}
	if cancelCalls != 1 {
		t.Fatalf("cancel calls = %d, want 1", cancelCalls)
	}
	if emit.EncErr() != nil {
		t.Fatalf("EncErr() = %v, want nil", emit.EncErr())
	}
}

func TestEncodingErrorDoesNotCancelOrStopSubsequentWrites(t *testing.T) {
	sink := testsse.NewSink()
	var cancelCalls int
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", func() {
		cancelCalls++
	})

	emit.write(invalidEvent{BaseEvent: events.NewBaseEvent(events.EventTypeCustom)})
	emit.RunStarted()

	if emit.Err() != nil {
		t.Fatalf("Err() = %v, want nil", emit.Err())
	}
	if emit.EncErr() == nil {
		t.Fatal("EncErr() is nil, want validation error")
	}
	if cancelCalls != 0 {
		t.Fatalf("cancel calls = %d, want 0", cancelCalls)
	}
	if got, want := golden.FrameTypes(normalizedFrames(t, sink)), []string{"RUN_STARTED"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
}

func TestIsTransportErrorMatchesOnlySDKTransportPrefixes(t *testing.T) {
	for _, err := range []error{
		errors.New("SSE write failed: broken pipe"),
		errors.New("SSE flush failed: broken pipe"),
	} {
		if !isTransportError(err) {
			t.Fatalf("isTransportError(%q) = false, want true", err)
		}
	}
	for _, err := range []error{
		nil,
		errors.New("event encoding failed: SSE write failed: not outer prefix"),
		errors.New("SSE frame creation failed: bad data"),
	} {
		if isTransportError(err) {
			t.Fatalf("isTransportError(%v) = true, want false", err)
		}
	}
}

func normalizedFrames(t *testing.T, sink *testsse.Sink) []golden.Frame {
	t.Helper()
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush sink: %v", err)
	}
	frames, err := golden.NormalizeSSE(sink.Bytes())
	if err != nil {
		t.Fatalf("normalize SSE: %v\n%s", err, sink.String())
	}
	return frames
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

type invalidEvent struct {
	*events.BaseEvent
}

func (invalidEvent) Validate() error {
	return errors.New("invalid event")
}

func readEmitterFixture(t *testing.T) emitterFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/golden/emitter.normalized.json")
	if err != nil {
		t.Fatalf("read emitter fixture: %v", err)
	}
	var fixture emitterFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode emitter fixture: %v", err)
	}
	return fixture
}

func comparableFrameData(frames []golden.Frame) []map[string]any {
	out := make([]map[string]any, 0, len(frames))
	for _, frame := range frames {
		out = append(out, comparableData(frame.Data))
	}
	return out
}

func fixtureFrameData(frames []emitterFixtureFrame) []map[string]any {
	out := make([]map[string]any, 0, len(frames))
	for _, frame := range frames {
		out = append(out, comparableData(frame.Data))
	}
	return out
}

func comparableData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for key, value := range data {
		switch key {
		case "timestamp":
			continue
		case "id", "messageId":
			out[key] = golden.MessageIDPlaceholder
		case "messages":
			out[key] = comparableMessages(value)
		default:
			out[key] = value
		}
	}
	return out
}

func comparableMessages(value any) any {
	messages, ok := value.([]any)
	if !ok {
		return value
	}
	out := make([]any, len(messages))
	for i, message := range messages {
		messageMap, ok := message.(map[string]any)
		if !ok {
			out[i] = message
			continue
		}
		next := make(map[string]any, len(messageMap))
		for key, child := range messageMap {
			if key == "id" {
				next[key] = golden.MessageIDPlaceholder
			} else {
				next[key] = child
			}
		}
		out[i] = next
	}
	return out
}

type emitterFixture struct {
	Input struct {
		Messages []types.Message `json:"messages"`
	} `json:"input"`
	Frames []emitterFixtureFrame `json:"frames"`
}

type emitterFixtureFrame struct {
	Data map[string]any `json:"data"`
}
