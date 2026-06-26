package stream

import (
	"context"
	"reflect"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/mattsp1290/eino-agui/emitter"
	"github.com/mattsp1290/eino-agui/internal/golden"
	"github.com/mattsp1290/eino-agui/internal/testids"
	"github.com/mattsp1290/eino-agui/internal/testmodel"
	"github.com/mattsp1290/eino-agui/internal/testsse"
)

func TestStreamTurnEmitsReasoningTextAndLiveToolCalls(t *testing.T) {
	testids.WithDeterministicGenerator(t, "stream")
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel(testmodel.MixedStreamChunks())

	msg, err := StreamTurn(context.Background(), emit, model, nil, WithLiveToolCallEvents(true))
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if msg.Content != "Hello world" {
		t.Fatalf("message content = %q, want Hello world", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call-weather" {
		t.Fatalf("message tool calls = %#v", msg.ToolCalls)
	}

	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{
		"REASONING_START",
		"REASONING_MESSAGE_START",
		"REASONING_MESSAGE_CONTENT",
		"REASONING_MESSAGE_END",
		"REASONING_END",
		"TEXT_MESSAGE_START",
		"TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_END",
		"TOOL_CALL_START",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_END",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
}

func TestStreamTurnLeavesToolCallsUnemittedWhenLiveToolCallsDisabled(t *testing.T) {
	testids.WithDeterministicGenerator(t, "stream")
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel(testmodel.ToolCallChunks(0, "call-weather", "get_weather", `{"city":"NYC"}`))

	msg, err := StreamTurn(context.Background(), emit, model, nil)
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("message tool calls = %#v, want one", msg.ToolCalls)
	}
	frames := normalizedFrames(t, sink)
	if len(frames) != 0 {
		t.Fatalf("frames = %#v, want none", frames)
	}
}

func TestStreamTurnBuffersToolStartUntilIDAndNameKnown(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel([]*schema.Message{
		toolCallChunk(0, "", "", `{"path":`),
		toolCallChunk(0, "tool-1", "", `"README.md"`),
		toolCallChunk(0, "", "file_read", ""),
	})

	if _, err := StreamTurn(context.Background(), emit, model, nil, WithLiveToolCallEvents(true)); err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{
		"TOOL_CALL_START",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_END",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
}

func TestStreamTurnKeysToolCallsByIndex(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel([]*schema.Message{
		toolCallChunk(0, "tool-a", "first", "a1"),
		toolCallChunk(1, "tool-b", "second", "b1"),
		toolCallChunk(0, "", "", "a2"),
		toolCallChunk(1, "", "", "b2"),
	})

	if _, err := StreamTurn(context.Background(), emit, model, nil, WithLiveToolCallEvents(true)); err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	frames := normalizedFrames(t, sink)
	if got, want := golden.FrameTypes(frames), []string{
		"TOOL_CALL_START",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_START",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_ARGS",
		"TOOL_CALL_END",
		"TOOL_CALL_END",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
}

func TestStreamTurnEmptyStreamReturnsError(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel(nil)

	if _, err := StreamTurn(context.Background(), emit, model, nil); err == nil {
		t.Fatal("StreamTurn error is nil, want empty model stream error")
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

func toolCallChunk(index int, id, name, args string) *schema.Message {
	return &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			Index: &index,
			ID:    id,
			Type:  "function",
			Function: schema.FunctionCall{
				Name:      name,
				Arguments: args,
			},
		}},
	}
}
