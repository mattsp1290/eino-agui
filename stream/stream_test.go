package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
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

	result, err := StreamTurn(context.Background(), emit, model, nil, WithLiveToolCallEvents(true))
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if result.Assistant.Content != "Hello world" {
		t.Fatalf("message content = %q, want Hello world", result.Assistant.Content)
	}
	if len(result.Assistant.ToolCalls) != 1 || result.Assistant.ToolCalls[0].ID != "call-weather" {
		t.Fatalf("message tool calls = %#v", result.Assistant.ToolCalls)
	}
	if result.Partial || result.ToolOwnerID == "" || len(result.WireMessages) != 3 {
		t.Fatalf("stream result = %#v", result)
	}
	owner := result.WireMessages[2]
	if owner.ID != result.ToolOwnerID || len(owner.ToolCalls) != 1 || owner.ToolCalls[0].ID != "call-weather" {
		t.Fatalf("tool owner = %#v", owner)
	}

	frames := normalizedFrames(t, sink)
	if !strings.Contains(sink.String(), `"parentMessageId":"`+result.ToolOwnerID+`"`) {
		t.Fatalf("tool start does not reference result owner %q: %s", result.ToolOwnerID, sink.String())
	}
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

	result, err := StreamTurn(context.Background(), emit, model, nil, WithLiveToolCallEvents(fixture.Input.StreamToolCalls))
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if result.Assistant.Content != "answer " {
		t.Fatalf("message content = %q, want answer ", result.Assistant.Content)
	}
	if len(result.Assistant.ToolCalls) != 1 || result.Assistant.ToolCalls[0].ID != "call-weather" {
		t.Fatalf("message tool calls = %#v", result.Assistant.ToolCalls)
	}

	frames := normalizedFrames(t, sink)
	if got, want := comparableFrameData(frames), fixtureFrameData(fixture.Frames); !reflect.DeepEqual(got, want) {
		t.Fatalf("frame data = %#v, want %#v", got, want)
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

	result, err := StreamTurn(context.Background(), emit, model, nil)
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if len(result.Assistant.ToolCalls) != 1 {
		t.Fatalf("message tool calls = %#v, want one", result.Assistant.ToolCalls)
	}
	if result.ToolOwnerID == "" || len(result.WireMessages) != 1 || result.WireMessages[0].ID != result.ToolOwnerID || len(result.WireMessages[0].ToolCalls) != 1 {
		t.Fatalf("disabled-live owner result = %#v", result)
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

	result, err := StreamTurn(context.Background(), emit, model, nil)
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if result.Assistant.Content != "hello world" {
		t.Fatalf("message content = %q, want hello world", result.Assistant.Content)
	}
	if got, want := result.Assistant.Extra["reasoning"], "first"; got != want {
		t.Fatalf("Extra[reasoning] = %v, want %v", got, want)
	}
	if got, want := result.Assistant.Extra["continuation"], "second"; got != want {
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

func TestStreamTurnDoesNotOverlapToolAndTextBlocks(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	chunks := append(testmodel.ToolCallChunks(0, "tool-1", "lookup", "{}"), testmodel.TextChunk("after"))
	if _, err := StreamTurn(context.Background(), emit, testmodel.NewReplayModel(chunks), nil, WithLiveToolCallEvents(true)); err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if got, want := golden.FrameTypes(normalizedFrames(t, sink)), []string{"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame types = %v, want %v", got, want)
	}
}

func TestStreamTurnRejectsAmbiguousToolCorrelation(t *testing.T) {
	tests := []struct {
		name   string
		chunks []*schema.Message
	}{
		{"changed ID", []*schema.Message{toolCallChunk(0, "tool-a", "first", "{}"), toolCallChunk(0, "tool-b", "", "")}},
		{"duplicate ID", []*schema.Message{toolCallChunk(0, "tool-a", "first", "{}"), toolCallChunk(1, "tool-a", "second", "{}")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.chunks[0].ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{TotalTokens: 7}}
			sink := testsse.NewSink()
			emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
			result, err := StreamTurn(context.Background(), emit, testmodel.NewReplayModel(tt.chunks), nil, WithLiveToolCallEvents(true))
			var correlationErr *CorrelationError
			if !errors.As(err, &correlationErr) {
				t.Fatalf("error = %v, want CorrelationError", err)
			}
			if result == nil || !result.Partial || result.ToolOwnerID == "" || result.Usage == nil || result.Usage.TotalTokens != 7 {
				t.Fatalf("partial result = %#v", result)
			}
			for _, message := range result.WireMessages {
				if len(message.ToolCalls) != 0 {
					t.Fatalf("ambiguous calls attached to wire message: %#v", message)
				}
			}
		})
	}
}

func TestStreamTurnUsageAndNilIndexCorrelation(t *testing.T) {
	usage := &schema.TokenUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}
	nilIndexCall := func(id, name, args string) *schema.Message {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}}
	}
	chunks := []*schema.Message{
		{Role: schema.Assistant, Content: "ok", ResponseMeta: &schema.ResponseMeta{Usage: usage}},
		nilIndexCall("", "", "discarded"),
		nilIndexCall("call-1", "lookup", "{"),
		nilIndexCall("call-1", "", "}"),
	}
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	result, err := StreamTurn(context.Background(), emit, testmodel.NewReplayModel(chunks), nil, WithLiveToolCallEvents(true))
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	if result.Usage == nil || result.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	frames := normalizedFrames(t, sink)
	var deltas []string
	for _, frame := range frames {
		if frame.Data["type"] == "TOOL_CALL_ARGS" {
			deltas = append(deltas, frame.Data["delta"].(string))
		}
	}
	if !reflect.DeepEqual(deltas, []string{"{", "}"}) {
		t.Fatalf("tool deltas = %#v, anonymous fragment was not discarded", deltas)
	}
}

func TestStreamTurnRetainsUsageAndClosesBlocksOnErrors(t *testing.T) {
	usage := &schema.TokenUsage{TotalTokens: 8}
	tests := []struct {
		name      string
		makeModel func(context.CancelFunc) model.ToolCallingChatModel
		wantError string
	}{
		{
			name: "receive error",
			makeModel: func(context.CancelFunc) model.ToolCallingChatModel {
				return readerModel{open: func() *schema.StreamReader[*schema.Message] {
					reader, writer := schema.Pipe[*schema.Message](2)
					writer.Send(&schema.Message{Role: schema.Assistant, Content: "partial", ResponseMeta: &schema.ResponseMeta{Usage: usage}}, nil)
					writer.Send(nil, errors.New("receive failed"))
					writer.Close()
					return reader
				}}
			},
			wantError: "receive failed",
		},
		{
			name: "context cancellation",
			makeModel: func(cancel context.CancelFunc) model.ToolCallingChatModel {
				return readerModel{open: func() *schema.StreamReader[*schema.Message] {
					reader := schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "partial", ResponseMeta: &schema.ResponseMeta{Usage: usage}}})
					return schema.StreamReaderWithConvert(reader, func(chunk *schema.Message) (*schema.Message, error) {
						cancel()
						return chunk, nil
					})
				}}
			},
			wantError: context.Canceled.Error(),
		},
		{
			name: "concatenation error",
			makeModel: func(context.CancelFunc) model.ToolCallingChatModel {
				return testmodel.NewReplayModel([]*schema.Message{
					{Role: schema.Assistant, Content: "partial", ResponseMeta: &schema.ResponseMeta{Usage: usage}},
					{Role: schema.User, Content: "wrong role"},
				})
			},
			wantError: "cannot concat messages",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sink := testsse.NewSink()
			emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
			result, err := StreamTurn(ctx, emit, tt.makeModel(cancel), nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantError)
			}
			if result == nil || !result.Partial || result.Usage == nil || result.Usage.TotalTokens != 8 {
				t.Fatalf("partial result = %#v", result)
			}
			frames := normalizedFrames(t, sink)
			got := golden.FrameTypes(frames)
			if len(got) < 3 || got[0] != "TEXT_MESSAGE_START" || got[len(got)-1] != "TEXT_MESSAGE_END" || golden.CountType(frames, "TEXT_MESSAGE_START") != 1 || golden.CountType(frames, "TEXT_MESSAGE_END") != 1 {
				t.Fatalf("unbalanced frames = %v", got)
			}
		})
	}
}

func TestStreamTurnReturnsPartialResultOnTerminalTransportError(t *testing.T) {
	emit := emitter.NewEmitter(context.Background(), bufio.NewWriter(streamErrorWriter{}), sse.NewSSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel([]*schema.Message{{
		Role: schema.Assistant, Content: "partial",
		ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{TotalTokens: 6}},
	}})
	result, err := StreamTurn(context.Background(), emit, model, nil)
	if err == nil || emit.Err() == nil {
		t.Fatalf("transport errors = returned %v, emitter %v", err, emit.Err())
	}
	if result == nil || !result.Partial || result.Usage == nil || result.Usage.TotalTokens != 6 {
		t.Fatalf("partial result = %#v", result)
	}
}

func TestStreamUsageMapsToRunEndings(t *testing.T) {
	usageChunk := &schema.Message{Role: schema.Assistant, Content: "done", ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}}}
	tests := []struct {
		name      string
		model     model.ToolCallingChatModel
		wantError bool
		finish    func(*emitter.Emitter, aguievents.TokenUsage)
		wantType  string
	}{
		{"success", testmodel.NewReplayModel([]*schema.Message{usageChunk}), false, func(e *emitter.Emitter, usage aguievents.TokenUsage) { e.RunFinishedSuccess(usage) }, "RUN_FINISHED"},
		{"interrupt", testmodel.NewReplayModel([]*schema.Message{usageChunk}), false, func(e *emitter.Emitter, usage aguievents.TokenUsage) {
			e.RunFinishedInterrupt([]aguitypes.Interrupt{{ID: "int-1", Reason: "approval"}}, usage)
		}, "RUN_FINISHED"},
		{"model receive error", readerModel{open: func() *schema.StreamReader[*schema.Message] {
			reader, writer := schema.Pipe[*schema.Message](2)
			writer.Send(usageChunk, nil)
			writer.Send(nil, errors.New("model receive failed"))
			writer.Close()
			return reader
		}}, true, func(e *emitter.Emitter, usage aguievents.TokenUsage) { e.RunError("model receive failed", usage) }, "RUN_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := testsse.NewSink()
			emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
			result, err := StreamTurn(context.Background(), emit, tt.model, nil)
			if (err != nil) != tt.wantError {
				t.Fatalf("StreamTurn error = %v, wantError %v", err, tt.wantError)
			}
			mapped, err := convert.ToAGUITokenUsage(result.Usage, "provider", "model")
			if err != nil || mapped == nil {
				t.Fatalf("mapped usage = %#v, %v", mapped, err)
			}
			tt.finish(emit, *mapped)
			frames := normalizedFrames(t, sink)
			ending := frames[len(frames)-1].Data
			if ending["type"] != tt.wantType {
				t.Fatalf("ending = %#v", ending)
			}
			wireUsage := ending["usage"].([]any)[0].(map[string]any)
			if wireUsage["inputTokens"] != float64(5) || wireUsage["outputTokens"] != float64(3) || wireUsage["totalTokens"] != float64(8) {
				t.Fatalf("ending usage = %#v", wireUsage)
			}
		})
	}
}

func TestStreamPackageDoesNotOwnPostTurnProposalEmission(t *testing.T) {
	data, err := os.ReadFile("stream.go")
	if err != nil {
		t.Fatalf("read stream.go: %v", err)
	}
	source := string(data)
	for _, forbidden := range []string{"emitToolProposal", "RunConfig", "ToolPolicy", "settlePendingToolCalls"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("stream.go contains app proposal/policy marker %q", forbidden)
		}
	}
	if !strings.Contains(source, "callers must not also emit post-turn") {
		t.Fatal("StreamTurn documentation must state the no-duplicate post-turn proposal contract")
	}
}

func TestStreamTurnEmptyStreamReturnsError(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	model := testmodel.NewReplayModel(nil)

	result, err := StreamTurn(context.Background(), emit, model, nil)
	if err == nil {
		t.Fatal("StreamTurn error is nil, want empty model stream error")
	}
	if result == nil || !result.Partial {
		t.Fatalf("empty stream result = %#v, want non-nil partial", result)
	}
}

func TestStreamTurnOpenFailureReturnsNilResult(t *testing.T) {
	sink := testsse.NewSink()
	emit := emitter.NewEmitter(context.Background(), sink.Writer(), sink.SSEWriter(), "thread-1", "run-1", nil)
	result, err := StreamTurn(context.Background(), emit, testmodel.NewScriptedModel(), nil)
	if err == nil || result != nil {
		t.Fatalf("StreamTurn = %#v, %v; want nil,error", result, err)
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
	indexes := map[int]*int{}
	for _, chunk := range chunks {
		msg := &schema.Message{
			Role:             schema.Assistant,
			Content:          chunk.Content,
			ReasoningContent: chunk.ReasoningContent,
		}
		for _, call := range chunk.ToolCalls {
			index := indexes[call.Index]
			if index == nil {
				value := call.Index
				index = &value
				indexes[call.Index] = index
			}
			msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
				Index: index,
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

func comparableFrameData(frames []golden.Frame) []map[string]any {
	out := make([]map[string]any, 0, len(frames))
	for _, frame := range frames {
		out = append(out, comparableData(frame.Data))
	}
	return out
}

func fixtureFrameData(frames []streamFixtureFrame) []map[string]any {
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
		case "messageId", "parentMessageId":
			out[key] = golden.MessageIDPlaceholder
		default:
			out[key] = value
		}
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
	Frames     []streamFixtureFrame `json:"frames"`
	Assertions struct {
		ToolCallStartCount int `json:"toolCallStartCount"`
	} `json:"assertions"`
}

type streamFixtureFrame struct {
	Data map[string]any `json:"data"`
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

type readerModel struct {
	open func() *schema.StreamReader[*schema.Message]
}

func (m readerModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("not implemented")
}

func (m readerModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.open(), nil
}

func (m readerModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) { return m, nil }

type streamErrorWriter struct{}

func (streamErrorWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
