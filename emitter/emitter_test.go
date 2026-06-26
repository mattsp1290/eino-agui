package emitter

import (
	"bufio"
	"context"
	"errors"
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

func TestToolCallBufferStartsOnceWhenIDAndNameKnown(t *testing.T) {
	sink := testsse.NewSink()
	emit := NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	call := emit.NewToolCallBuffer()

	call.Update("", "", `{"path":`)
	call.Update("tool-1", "", `"README.md"`)
	call.Update("", "file_read", "")
	call.End()
	call.End()

	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{
		"TOOL_CALL_START",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_END",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
	if got := golden.CountType(frames, "TOOL_CALL_START"); got != 1 {
		t.Fatalf("TOOL_CALL_START count = %d, want 1", got)
	}
	if got := golden.CountType(frames, "TOOL_CALL_END"); got != 1 {
		t.Fatalf("TOOL_CALL_END count = %d, want 1", got)
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
