package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/emitter"
	"github.com/mattsp1290/eino-agui/internal/golden"
	"github.com/mattsp1290/eino-agui/internal/testmodel"
	"github.com/mattsp1290/eino-agui/internal/testsse"
)

type readerAgenticModel struct {
	reader func() *schema.StreamReader[*schema.AgenticMessage]
}

type contextClosingAgenticModel struct {
	reader *schema.StreamReader[*schema.AgenticMessage]
	writer *schema.StreamWriter[*schema.AgenticMessage]
}

type sliceMutatingAgenticModel struct {
	chunks []*schema.AgenticMessage
}

func (m sliceMutatingAgenticModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return nil, errors.New("not used")
}
func (m sliceMutatingAgenticModel) Stream(_ context.Context, input []*schema.AgenticMessage, options ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	if len(input) != 0 {
		input[0] = nil
	}
	if len(options) != 0 {
		options[0] = model.Option{}
	}
	return schema.StreamReaderFromArray(m.chunks), nil
}

func (m readerAgenticModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return nil, errors.New("not used")
}
func (m readerAgenticModel) Stream(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return m.reader(), nil
}

func (m contextClosingAgenticModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return nil, errors.New("not used")
}
func (m contextClosingAgenticModel) Stream(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	go func() {
		<-ctx.Done()
		m.writer.Close()
	}()
	return m.reader, nil
}

type testBlockResolver map[int]convert.AgenticBlockContext

func (r testBlockResolver) ResolveBlock(index int, _ schema.ContentBlockType) (convert.AgenticBlockContext, error) {
	value, ok := r[index]
	if !ok {
		return convert.AgenticBlockContext{}, errors.New("missing context")
	}
	return value, nil
}

type recordingSink struct {
	events    []events.Event
	failAt    int
	detached  int
	detachErr error
}

type notifyingSink struct {
	notified chan struct{}
	once     sync.Once
}

func (s *notifyingSink) Emit(events.Event) error {
	s.once.Do(func() { close(s.notified) })
	return nil
}
func (s *notifyingSink) Detach(error) {}

func (s *recordingSink) Emit(event events.Event) error {
	if s.failAt > 0 && len(s.events)+1 == s.failAt {
		return errors.New("observer failed")
	}
	s.events = append(s.events, event)
	return nil
}
func (s *recordingSink) Detach(err error) { s.detached++; s.detachErr = err }

func agenticIDs() AgenticStreamIdentity {
	return AgenticStreamIdentity{SessionID: "session", RunID: "run", TurnID: "turn", MessageID: "message", AttemptID: "attempt", AgentPath: []convert.AgentPathSegment{{Name: "root", RunID: "run"}}}
}

func TestStreamAgenticTurnCorrelatesIndexedChunks(t *testing.T) {
	t.Parallel()
	chunks := append(testmodel.AgenticTextChunks(0, "hello "), testmodel.AgenticToolCallChunks(1, "call", "lookup", `{"q":`, `"go"}`)...)
	chunks = append(chunks, testmodel.AgenticTextChunks(0, "world")...)
	model := testmodel.NewAgenticReplayModel(chunks)
	sink := &recordingSink{}
	result, err := StreamAgenticTurn(t.Context(), model, nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}, 1: {BlockID: "tool"}}, WithTransientSink(sink))
	if err != nil {
		t.Fatalf("StreamAgenticTurn() error = %v", err)
	}
	if result.Partial || result.Terminal != AgenticTerminalEOF || result.PublicProjection == nil {
		t.Fatalf("result = %#v", result)
	}
	if got, want := result.SeenBlockIndices, []int{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("indices = %v, want %v", got, want)
	}
	if got := *result.PublicProjection.Blocks[0].Public.Text; got != "hello world" {
		t.Fatalf("text = %q", got)
	}
	if got := result.PublicProjection.Blocks[1].Public.FunctionToolCall.Arguments; got != `{"q":"go"}` {
		t.Fatalf("arguments = %q", got)
	}
	if len(result.DeliveredTransient) != len(chunks) || len(sink.events) != len(chunks) {
		t.Fatalf("transient counts result=%d sink=%d chunks=%d", len(result.DeliveredTransient), len(sink.events), len(chunks))
	}
	for _, event := range sink.events {
		identity := event.GetBaseEvent().Metadata[convert.AgenticCustomEventName].(convert.AgenticIdentityV1)
		if !identity.Transient {
			t.Fatal("transient event lacks marker")
		}
	}
}

func TestStreamAgenticTurnAllAssistantContentKinds(t *testing.T) {
	t.Parallel()
	code := int64(7)
	tests := []struct {
		name    string
		block   *schema.ContentBlock
		context convert.AgenticBlockContext
	}{
		{"reasoning", schema.NewContentBlock(&schema.Reasoning{Text: "why"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant text", schema.NewContentBlock(&schema.AssistantGenText{Text: "answer"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant image", schema.NewContentBlock(&schema.AssistantGenImage{URL: "https://example.test/image"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant audio", schema.NewContentBlock(&schema.AssistantGenAudio{URL: "https://example.test/audio"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant video", schema.NewContentBlock(&schema.AssistantGenVideo{URL: "https://example.test/video"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"function call", schema.NewContentBlock(&schema.FunctionToolCall{CallID: "function", Name: "lookup", Arguments: `{}`}), convert.AgenticBlockContext{BlockID: "block"}},
		{"server call", schema.NewContentBlock(&schema.ServerToolCall{CallID: "server", Name: "search", Arguments: map[string]any{"q": "go"}}), convert.AgenticBlockContext{BlockID: "block", ProviderServerID: "provider"}},
		{"server result", schema.NewContentBlock(&schema.ServerToolResult{CallID: "server", Name: "search", Content: []any{"result"}}), convert.AgenticBlockContext{BlockID: "block", ProviderServerID: "provider"}},
		{"MCP call", schema.NewContentBlock(&schema.MCPToolCall{ServerLabel: "server", CallID: "mcp", Name: "lookup", Arguments: `{}`}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP result", schema.NewContentBlock(&schema.MCPToolResult{ServerLabel: "server", CallID: "mcp", Name: "lookup", Content: `{}`, Error: &schema.MCPToolCallError{Code: &code, Message: "failed"}}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP list", schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{{Name: "lookup"}}}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP approval request", schema.NewContentBlock(&schema.MCPToolApprovalRequest{ID: "approval", ServerLabel: "server", Name: "lookup", Arguments: `{}`}), convert.AgenticBlockContext{BlockID: "block"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block := *tc.block
			block.StreamingMeta = &schema.StreamingMeta{Index: 0}
			message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{&block}}
			result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel([]*schema.AgenticMessage{message}), nil, agenticIDs(), testBlockResolver{0: tc.context})
			if err != nil || result.PublicProjection == nil || len(result.PublicProjection.Public.ContentBlocks) != 1 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if got := result.PublicProjection.Public.ContentBlocks[0].Type; got != block.Type {
				t.Fatalf("type = %q, want %q", got, block.Type)
			}
		})
	}
}

func TestStreamAgenticTurnDetachesObserverAndFinishes(t *testing.T) {
	t.Parallel()
	chunks := testmodel.AgenticTextChunks(0, "one", "two", "three")
	sink := &recordingSink{failAt: 2}
	result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(chunks), nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}}, WithTransientSink(sink))
	if err != nil {
		t.Fatalf("execution error = %v", err)
	}
	if result.PublicProjection == nil || result.ObserverErr == nil || sink.detached != 1 {
		t.Fatalf("result=%#v detached=%d", result, sink.detached)
	}
	if len(result.DeliveredTransient) != 1 {
		t.Fatalf("delivered = %d, want 1", len(result.DeliveredTransient))
	}
}

func TestStreamAgenticTurnThroughRealObserverEmitter(t *testing.T) {
	t.Parallel()
	sseSink := testsse.NewSink()
	observer := emitter.NewObserverSink(emitter.NewObserverEmitter(t.Context(), sseSink.Writer(), sseSink.SSEWriter()))
	result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(testmodel.AgenticTextChunks(0, "one", "two")), nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}}, WithTransientSink(observer))
	if err != nil || result.PublicProjection == nil || result.ObserverErr != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if err := sseSink.Flush(); err != nil {
		t.Fatal(err)
	}
	frames, err := golden.NormalizeSSE(sseSink.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := golden.FrameTypes(frames); !reflect.DeepEqual(got, []string{"TEXT_MESSAGE_CHUNK", "TEXT_MESSAGE_CHUNK"}) {
		t.Fatalf("types = %v", got)
	}
	for _, frame := range frames {
		identity := frame.Data["metadata"].(map[string]any)[convert.AgenticCustomEventName].(map[string]any)
		if identity["transient"] != true {
			t.Fatalf("identity = %#v", identity)
		}
	}
}

func TestStreamAgenticTurnCancelledObserverDoesNotCancelExecution(t *testing.T) {
	t.Parallel()
	observerCtx, cancelObserver := context.WithCancel(context.Background())
	cancelObserver()
	sseSink := testsse.NewSink()
	observer := emitter.NewObserverSink(emitter.NewObserverEmitter(observerCtx, sseSink.Writer(), sseSink.SSEWriter()))
	model := testmodel.NewAgenticReplayModel(testmodel.AgenticTextChunks(0, "one", "two"))
	result, err := StreamAgenticTurn(t.Context(), model, nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}}, WithTransientSink(observer))
	if err != nil || result.PublicProjection == nil || !errors.Is(result.ObserverErr, context.Canceled) || result.Terminal != AgenticTerminalEOF {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if model.Calls() != 1 || len(result.DeliveredTransient) != 0 {
		t.Fatalf("model calls=%d delivered=%d", model.Calls(), len(result.DeliveredTransient))
	}
	if err := sseSink.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(sseSink.Bytes()) != 0 {
		t.Fatalf("cancelled observer bytes = %q", sseSink.Bytes())
	}
}

func TestStreamAgenticTurnLateMalformedChunkReturnsPartialPrefix(t *testing.T) {
	t.Parallel()
	chunks := testmodel.AgenticTextChunks(0, "safe")
	chunks = append(chunks, &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{{Type: schema.ContentBlockTypeReasoning, Reasoning: &schema.Reasoning{Text: "x"}, AssistantGenText: &schema.AssistantGenText{Text: "bad"}, StreamingMeta: &schema.StreamingMeta{Index: 1}}}})
	sink := &recordingSink{}
	result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(chunks), nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}, 1: {BlockID: "bad"}}, WithTransientSink(sink))
	if err == nil {
		t.Fatal("malformed chunk was accepted")
	}
	if !result.Partial || result.PublicProjection != nil || len(result.DeliveredTransient) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestStreamAgenticTurnOpenAndEmptyErrors(t *testing.T) {
	t.Parallel()
	openErr := errors.New("open")
	result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticOpenErrorModel(openErr), nil, agenticIDs(), testBlockResolver{})
	if !errors.Is(err, openErr) || result != nil {
		t.Fatalf("open result=%#v err=%v", result, err)
	}
	result, err = StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(nil), nil, agenticIDs(), testBlockResolver{})
	if err == nil || result == nil || !result.Partial {
		t.Fatalf("empty result=%#v err=%v", result, err)
	}
}

func TestStreamAgenticTurnRejectsKindConflict(t *testing.T) {
	t.Parallel()
	chunks := testmodel.AgenticTextChunks(0, "safe")
	chunks = append(chunks, &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.Reasoning{Text: "conflict"}, &schema.StreamingMeta{Index: 0})}})
	result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(chunks), nil, agenticIDs(), testBlockResolver{0: {BlockID: "block"}})
	if err == nil || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestStreamAgenticTurnHonorsWithOnEOFFinalValueAndError(t *testing.T) {
	t.Parallel()
	makeReader := func(onEOF func() (any, error)) *schema.StreamReader[*schema.AgenticMessage] {
		return schema.StreamReaderWithConvert(
			schema.StreamReaderFromArray(testmodel.AgenticTextChunks(0, "first")),
			func(message *schema.AgenticMessage) (*schema.AgenticMessage, error) { return message, nil },
			schema.WithOnEOF(onEOF),
		)
	}
	final := testmodel.AgenticTextChunks(0, " final")[0]
	result, err := StreamAgenticTurn(t.Context(), readerAgenticModel{reader: func() *schema.StreamReader[*schema.AgenticMessage] {
		return makeReader(func() (any, error) { return final, nil })
	}}, nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}})
	if err != nil || result.PublicProjection == nil || *result.PublicProjection.Blocks[0].Public.Text != "first final" {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	eofErr := errors.New("on eof failed")
	result, err = StreamAgenticTurn(t.Context(), readerAgenticModel{reader: func() *schema.StreamReader[*schema.AgenticMessage] {
		return makeReader(func() (any, error) { return nil, eofErr })
	}}, nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}})
	if !errors.Is(err, eofErr) || result == nil || !result.Partial || result.PublicProjection != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	result, err = StreamAgenticTurn(t.Context(), readerAgenticModel{reader: func() *schema.StreamReader[*schema.AgenticMessage] {
		return makeReader(func() (any, error) { return nil, io.EOF })
	}}, nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}})
	if err != nil || result.PublicProjection == nil {
		t.Fatalf("ordinary EOF result=%#v err=%v", result, err)
	}
}

func TestStreamAgenticTurnEnforcesChunkLimit(t *testing.T) {
	t.Parallel()
	limits := convert.DefaultProjectionLimits()
	limits.MaxStreamChunks = 1
	result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(testmodel.AgenticTextChunks(0, "one", "two")), nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}}, WithProjectionLimits(limits))
	if err == nil || result == nil || !result.Partial || result.PublicProjection != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestStreamAgenticTurnCancellationUnblocksAndClosesReader(t *testing.T) {
	t.Parallel()
	reader, writer := schema.Pipe[*schema.AgenticMessage](1)
	if closed := writer.Send(testmodel.AgenticTextChunks(0, "partial")[0], nil); closed {
		t.Fatal("reader closed before drain")
	}
	ctx, cancel := context.WithCancel(t.Context())
	sink := &notifyingSink{notified: make(chan struct{})}
	done := make(chan struct{})
	var result *AgenticResult
	var err error
	go func() {
		defer close(done)
		result, err = StreamAgenticTurn(ctx, contextClosingAgenticModel{reader: reader, writer: writer}, nil, agenticIDs(), testBlockResolver{0: {BlockID: "text"}}, WithTransientSink(sink))
	}()
	select {
	case <-sink.notified:
	case <-time.After(time.Second):
		t.Fatal("first chunk was not drained")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not unblock the reader")
	}
	if !errors.Is(err, context.Canceled) || result == nil || !result.Partial || result.Terminal != AgenticTerminalCancelled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if closed := writer.Send(testmodel.AgenticTextChunks(0, "late")[0], nil); !closed {
		t.Fatal("reader was not closed after cancellation")
	}
}

func TestStreamAgenticTurnEnforcesBlockAndByteLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		chunks   []*schema.AgenticMessage
		limits   func() convert.ProjectionLimits
		resolver testBlockResolver
	}{
		{
			name:   "block count",
			chunks: append(testmodel.AgenticTextChunks(0, "one"), testmodel.AgenticTextChunks(1, "two")...),
			limits: func() convert.ProjectionLimits {
				limits := convert.DefaultProjectionLimits()
				limits.MaxBlocks = 1
				return limits
			},
			resolver: testBlockResolver{0: {BlockID: "one"}, 1: {BlockID: "two"}},
		},
		{
			name:   "block bytes",
			chunks: testmodel.AgenticTextChunks(0, "this exceeds the configured block byte limit"),
			limits: func() convert.ProjectionLimits {
				limits := convert.DefaultProjectionLimits()
				limits.MaxBlockBytes = 8
				return limits
			},
			resolver: testBlockResolver{0: {BlockID: "text"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(tc.chunks), nil, agenticIDs(), tc.resolver, WithProjectionLimits(tc.limits()), WithTransientSink(sink))
			if err == nil || result == nil || !result.Partial || result.PublicProjection != nil {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if tc.name == "block count" && len(result.DeliveredTransient) != 1 {
				t.Fatalf("delivered transient blocks = %d, want one in-limit prefix", len(result.DeliveredTransient))
			}
		})
	}
}

func TestStreamAgenticTurnDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	input := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: "unchanged"})}}}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel(testmodel.AgenticTextChunks(0, "answer")), input, agenticIDs(), testBlockResolver{0: {BlockID: "text"}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("input mutated:\nbefore %s\nafter  %s", before, after)
	}
}

func TestStreamAgenticTurnCopiesInputAndOptionSlices(t *testing.T) {
	t.Parallel()
	input := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.UserInputText{Text: "unchanged"})}}}
	options := []model.Option{model.WithTemperature(0.25)}
	result, err := StreamAgenticTurn(
		t.Context(),
		sliceMutatingAgenticModel{chunks: testmodel.AgenticTextChunks(0, "answer")},
		input,
		agenticIDs(),
		testBlockResolver{0: {BlockID: "text"}},
		WithAgenticModelOptions(options...),
	)
	if err != nil || result.PublicProjection == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if input[0] == nil || input[0].ContentBlocks[0].UserInputText.Text != "unchanged" {
		t.Fatalf("input slice was mutated: %#v", input)
	}
	gotOptions := model.GetCommonOptions(nil, options...)
	if gotOptions.Temperature == nil || *gotOptions.Temperature != 0.25 {
		t.Fatalf("option slice was mutated: %#v", gotOptions)
	}
}

func TestStreamAgenticTurnRejectsNilChunk(t *testing.T) {
	t.Parallel()
	result, err := StreamAgenticTurn(t.Context(), testmodel.NewAgenticReplayModel([]*schema.AgenticMessage{nil}), nil, agenticIDs(), testBlockResolver{})
	if err == nil || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
