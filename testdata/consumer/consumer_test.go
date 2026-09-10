package consumer_test

import (
	"bufio"
	"bytes"
	"context"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/emitter"
	bridge "github.com/mattsp1290/eino-agui/stream"
)

type agenticModel struct{ chunks []*schema.AgenticMessage }

func (m agenticModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return schema.ConcatAgenticMessages(m.chunks)
}
func (m agenticModel) Stream(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return schema.StreamReaderFromArray(m.chunks), nil
}

type blockResolver struct{}

func (blockResolver) ResolveBlock(index int, _ schema.ContentBlockType) (convert.AgenticBlockContext, error) {
	return convert.AgenticBlockContext{BlockID: "block"}, nil
}

type eventResolver struct{ ids bridge.AgenticStreamIdentity }

func (r eventResolver) ResolveAgentEvent(bridge.AgentEventCoordinates) (bridge.AgentEventResolution, error) {
	return bridge.AgentEventResolution{Identity: r.ids, Blocks: blockResolver{}}, nil
}

type sink struct{ events []events.Event }

func (s *sink) Emit(event events.Event) error { s.events = append(s.events, event); return nil }
func (*sink) Detach(error)                    {}

func TestDownloadedAgenticBridge(t *testing.T) {
	ids := bridge.AgenticStreamIdentity{SessionID: "session", RunID: "run", TurnID: "turn", MessageID: "message", AttemptID: "attempt", AgentPath: []convert.AgentPathSegment{{Name: "root", RunID: "run"}}}
	chunks := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.AssistantGenText{Text: "hello"}, &schema.StreamingMeta{Index: 0})}}}
	observer := &sink{}
	result, err := bridge.StreamAgenticTurn(t.Context(), agenticModel{chunks}, nil, ids, blockResolver{}, bridge.WithTransientSink(observer))
	if err != nil || result.PublicProjection == nil || len(observer.events) != 1 {
		t.Fatalf("stream result=%#v events=%d err=%v", result, len(observer.events), err)
	}

	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	emit := emitter.NewObserverEmitter(t.Context(), writer, sse.NewSSEWriter())
	receipt := convert.CommitReceiptV1{Revision: "revision", Domain: "projection", Identity: result.PublicProjection.Public.Identity, Digest: result.PublicProjection.Digest}
	if !emit.EmitCommittedProjection(result.PublicProjection, receipt, emitter.DeliveryModeLiveContinuation) {
		t.Fatalf("emit errors: %v / %v", emit.Err(), emit.EncErr())
	}
	writer.Flush()
	if !bytes.Contains(output.Bytes(), []byte(convert.AgenticCustomEventName)) {
		t.Fatal("missing custom envelope")
	}

	iterator, generator := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	generator.Send(adk.EventFromAgenticMessage(result.Assistant, nil, schema.AgenticRoleTypeAssistant))
	generator.Close()
	source, err := bridge.NewAgentEventSource(iterator, func(error) {}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	adkResult, err := bridge.DrainAgenticEvents(t.Context(), source, eventResolver{ids: ids})
	if err != nil || len(adkResult.Projections) != 1 {
		t.Fatalf("ADK result=%#v err=%v", adkResult, err)
	}
}
