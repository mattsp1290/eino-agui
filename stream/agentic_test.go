package stream

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/internal/testmodel"
)

type readerAgenticModel struct {
	reader func() *schema.StreamReader[*schema.AgenticMessage]
}

func (m readerAgenticModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return nil, errors.New("not used")
}
func (m readerAgenticModel) Stream(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return m.reader(), nil
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
