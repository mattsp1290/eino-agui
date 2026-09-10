package consumer_test

import (
	"bufio"
	"bytes"
	"context"
	"reflect"
	"testing"

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

type eventResolver struct {
	ids         bridge.AgenticStreamIdentity
	pauseID     string
	correlation *convert.ApprovalInterruptCorrelation
}

func (r eventResolver) ResolveAgentEvent(bridge.AgentEventCoordinates) (bridge.AgentEventResolution, error) {
	return bridge.AgentEventResolution{
		Identity: r.ids, Blocks: blockResolver{}, PauseID: r.pauseID, Correlation: r.correlation,
	}, nil
}

func TestDownloadedAgenticBridge(t *testing.T) {
	ids := bridge.AgenticStreamIdentity{SessionID: "session", RunID: "run", TurnID: "turn", MessageID: "message", AttemptID: "attempt", AgentPath: []convert.AgentPathSegment{{Name: "root", RunID: "run"}}}
	chunks := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.AssistantGenText{Text: "hello"}, &schema.StreamingMeta{Index: 0})}}}
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	emit := emitter.NewObserverEmitter(t.Context(), writer, sse.NewSSEWriter())
	observer := emitter.NewObserverSink(emit)
	result, err := bridge.StreamAgenticTurn(t.Context(), agenticModel{chunks}, nil, ids, blockResolver{}, bridge.WithTransientSink(observer))
	if err != nil || result.PublicProjection == nil || len(result.DeliveredTransient) != 1 {
		t.Fatalf("stream result=%#v err=%v", result, err)
	}
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

	iterator, generator = adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	generator.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{Action: &adk.AgentAction{Interrupted: &adk.InterruptInfo{
		InterruptContexts: []*adk.InterruptCtx{{ID: "interrupt", Address: adk.Address{{Type: adk.AddressSegmentAgent, ID: "root"}}}},
	}}})
	generator.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{Action: adk.NewTransferToAgentAction("research")})
	generator.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{Action: adk.NewExitAction()})
	generator.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{Action: adk.NewBreakLoopAction("root")})
	generator.Close()
	source, err = bridge.NewAgentEventSource(iterator, func(error) {}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	correlation := &convert.ApprovalInterruptCorrelation{
		ApprovalRequestID: "approval", InterruptTargetID: "interrupt", InterruptAddress: "agent:root",
	}
	observations, err := bridge.DrainAgenticEvents(t.Context(), source, eventResolver{
		ids: ids, pauseID: "pause", correlation: correlation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(observations.Interrupts) != 1 || !reflect.DeepEqual(observations.Interrupts[0].Identity, ids) ||
		observations.Interrupts[0].Pause.PauseID != "pause" ||
		!reflect.DeepEqual(observations.Interrupts[0].Pause.Correlation, correlation) {
		t.Fatalf("interrupt observations=%#v", observations.Interrupts)
	}
	wantControls := []bridge.AgentControlObservation{
		{Kind: bridge.AgentControlTransfer, Destination: "research", Identity: ids},
		{Kind: bridge.AgentControlExit, Identity: ids},
		{Kind: bridge.AgentControlBreakLoop, Identity: ids},
	}
	if !reflect.DeepEqual(observations.Controls, wantControls) {
		t.Fatalf("control observations=%#v, want %#v", observations.Controls, wantControls)
	}
}
