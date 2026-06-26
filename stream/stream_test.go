package stream

import (
	"context"
	"encoding/json"
	"os"
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

func TestStreamTurnMatchesNormalizedGoldenFixture(t *testing.T) {
	testids.WithDeterministicGenerator(t, "stream")
	fixture := readStreamTurnFixture(t)
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel(streamFixtureChunks(fixture.Input.Chunks))

	msg, err := StreamTurn(context.Background(), emit, model, nil, WithLiveToolCallEvents(fixture.Input.StreamToolCalls))
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if msg.Content != "answer " {
		t.Fatalf("message content = %q, want answer ", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call-weather" {
		t.Fatalf("message tool calls = %#v", msg.ToolCalls)
	}

	frames := normalizedFrames(t, sink)
	wantTypes := make([]string, 0, len(fixture.Frames))
	for _, frame := range fixture.Frames {
		wantTypes = append(wantTypes, frame.Data.Type)
	}
	if got := golden.FrameTypes(frames); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("frame types = %v, want %v", got, wantTypes)
	}
	if got := golden.CountType(frames, "TOOL_CALL_START"); got != fixture.Assertions.ToolCallStartCount {
		t.Fatalf("TOOL_CALL_START count = %d, want %d", got, fixture.Assertions.ToolCallStartCount)
	}
	assertToolStartAfterTextAndReasoningClose(t, frames)
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

func TestStreamTurnConcatPreservesExtra(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel([]*schema.Message{
		{Role: schema.Assistant, Content: "hello ", Extra: map[string]any{"reasoning": "first"}},
		{Role: schema.Assistant, Content: "world", Extra: map[string]any{"continuation": "second"}},
	})

	msg, err := StreamTurn(context.Background(), emit, model, nil)
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if msg.Content != "hello world" {
		t.Fatalf("message content = %q, want hello world", msg.Content)
	}
	if got, want := msg.Extra["reasoning"], "first"; got != want {
		t.Fatalf("Extra[reasoning] = %v, want %v", got, want)
	}
	if got, want := msg.Extra["continuation"], "second"; got != want {
		t.Fatalf("Extra[continuation] = %v, want %v", got, want)
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

func TestLiveToolCallsAreNotReEmittedAsPostTurnProposal(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel(testmodel.ToolCallChunks(0, "call-weather", "get_weather", `{"city":"NYC"}`))

	if _, err := StreamTurn(context.Background(), emit, model, nil, WithLiveToolCallEvents(true)); err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	frames := normalizedFrames(t, sink)
	if got := golden.CountType(frames, "TOOL_CALL_START"); got != 1 {
		t.Fatalf("TOOL_CALL_START count = %d, want 1", got)
	}
	if got := golden.CountType(frames, "TOOL_CALL_END"); got != 1 {
		t.Fatalf("TOOL_CALL_END count = %d, want 1", got)
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

func readStreamTurnFixture(t *testing.T) streamTurnFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/golden/stream_turn.normalized.json")
	if err != nil {
		t.Fatalf("read stream fixture: %v", err)
	}
	var fixture streamTurnFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode stream fixture: %v", err)
	}
	return fixture
}

func streamFixtureChunks(chunks []streamFixtureChunk) []*schema.Message {
	out := make([]*schema.Message, 0, len(chunks))
	for _, chunk := range chunks {
		msg := &schema.Message{
			Role:             schema.Assistant,
			Content:          chunk.Content,
			ReasoningContent: chunk.ReasoningContent,
		}
		for _, call := range chunk.ToolCalls {
			index := call.Index
			msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
				Index: &index,
				ID:    call.ID,
				Type:  "function",
				Function: schema.FunctionCall{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
		out = append(out, msg)
	}
	return out
}

func assertToolStartAfterTextAndReasoningClose(t *testing.T, frames []golden.Frame) {
	t.Helper()
	var toolStart, lastTextEnd, lastReasoningEnd int
	for i, frame := range frames {
		switch frame.Data["type"] {
		case "TOOL_CALL_START":
			if toolStart == 0 {
				toolStart = i + 1
			}
		case "TEXT_MESSAGE_END":
			lastTextEnd = i + 1
		case "REASONING_END":
			lastReasoningEnd = i + 1
		}
	}
	if toolStart == 0 {
		t.Fatal("TOOL_CALL_START not found")
	}
	if lastTextEnd == 0 || lastTextEnd > toolStart {
		t.Fatalf("last TEXT_MESSAGE_END index = %d, tool start = %d", lastTextEnd, toolStart)
	}
	if lastReasoningEnd == 0 || lastReasoningEnd > toolStart {
		t.Fatalf("last REASONING_END index = %d, tool start = %d", lastReasoningEnd, toolStart)
	}
}

type streamTurnFixture struct {
	Input struct {
		StreamToolCalls bool                 `json:"streamToolCalls"`
		Chunks          []streamFixtureChunk `json:"chunks"`
	} `json:"input"`
	Frames []struct {
		Data struct {
			Type string `json:"type"`
		} `json:"data"`
	} `json:"frames"`
	Assertions struct {
		ToolCallStartCount int `json:"toolCallStartCount"`
	} `json:"assertions"`
}

type streamFixtureChunk struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoningContent"`
	ToolCalls        []struct {
		Index     int    `json:"index"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"toolCalls"`
}
