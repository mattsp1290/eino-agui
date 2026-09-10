package stream

import (
	"bufio"
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/emitter"
	"github.com/mattsp1290/eino-agui/internal/testmodel"
	"github.com/mattsp1290/eino-agui/internal/testsse"
)

type testEventResolver struct{}

func (testEventResolver) ResolveAgentEvent(AgentEventCoordinates) (AgentEventResolution, error) {
	return AgentEventResolution{Identity: agenticIDs(), Blocks: testBlockResolver{0: {BlockID: "block"}}}, nil
}

type testEventResolverFunc func(AgentEventCoordinates) (AgentEventResolution, error)

func (f testEventResolverFunc) ResolveAgentEvent(coordinates AgentEventCoordinates) (AgentEventResolution, error) {
	return f(coordinates)
}

type scriptedAgentEventSource struct {
	events      []*adk.TypedAgentEvent[*schema.AgenticMessage]
	terminalErr error
	release     <-chan struct{}
	next        int
	aborts      atomic.Int32
	waits       atomic.Int32
}

func (s *scriptedAgentEventSource) Next(ctx context.Context) (*adk.TypedAgentEvent[*schema.AgenticMessage], bool, error) {
	if s.next < len(s.events) {
		event := s.events[s.next]
		s.next++
		return event, true, nil
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return nil, false, s.terminalErr
}

func (s *scriptedAgentEventSource) Abort(error) { s.aborts.Add(1) }
func (s *scriptedAgentEventSource) Wait(context.Context) error {
	s.waits.Add(1)
	return nil
}

type notifyingErrorWriter struct {
	failed chan struct{}
	once   sync.Once
}

func (w *notifyingErrorWriter) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.failed) })
	return 0, errors.New("broken pipe")
}

func sourceFromEvents(t *testing.T, events ...*adk.TypedAgentEvent[*schema.AgenticMessage]) AgentEventSource {
	t.Helper()
	iterator, generator := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	for _, event := range events {
		generator.Send(event)
	}
	generator.Close()
	source, err := NewAgentEventSource(iterator, func(error) {}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestDrainAgenticEventsCompleteAndStreamingMessages(t *testing.T) {
	t.Parallel()
	complete := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "complete"})}}
	completeEvent := adk.EventFromAgenticMessage(complete, nil, schema.AgenticRoleTypeAssistant)
	completeEvent.AgentName = "root"
	streamReader := schema.StreamReaderFromArray(testmodel.AgenticTextChunks(0, "stream", "ed"))
	streamEvent := adk.EventFromAgenticMessage(nil, streamReader, schema.AgenticRoleTypeAssistant)
	streamEvent.AgentName = "root"
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, completeEvent, streamEvent), testEventResolver{})
	if err != nil {
		t.Fatalf("DrainAgenticEvents() error = %v", err)
	}
	if result.Partial || len(result.Projections) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if got := *result.Projections[1].Blocks[0].Public.Text; got != "streamed" {
		t.Fatalf("streamed text = %q", got)
	}
}

func TestDrainAgenticEventsStreamsAllUserContentKinds(t *testing.T) {
	t.Parallel()
	resultParts := []*schema.FunctionToolResultContentBlock{
		{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "text"}},
		{Type: schema.FunctionToolResultContentBlockTypeImage, Image: &schema.UserInputImage{URL: "https://example.test/image"}},
		{Type: schema.FunctionToolResultContentBlockTypeAudio, Audio: &schema.UserInputAudio{URL: "https://example.test/audio"}},
		{Type: schema.FunctionToolResultContentBlockTypeVideo, Video: &schema.UserInputVideo{URL: "https://example.test/video"}},
		{Type: schema.FunctionToolResultContentBlockTypeFile, File: &schema.UserInputFile{URL: "https://example.test/file", Name: "file.pdf"}},
	}
	tests := []struct {
		name    string
		block   *schema.ContentBlock
		context convert.AgenticBlockContext
	}{
		{"user text", schema.NewContentBlock(&schema.UserInputText{Text: "hello"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user image", schema.NewContentBlock(&schema.UserInputImage{URL: "https://example.test/image"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user audio", schema.NewContentBlock(&schema.UserInputAudio{URL: "https://example.test/audio"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user video", schema.NewContentBlock(&schema.UserInputVideo{URL: "https://example.test/video"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user file", schema.NewContentBlock(&schema.UserInputFile{URL: "https://example.test/file", Name: "file.pdf"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"tool search", schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{{Name: "lookup"}}}}), convert.AgenticBlockContext{BlockID: "block"}},
		{"function result", schema.NewContentBlock(&schema.FunctionToolResult{CallID: "function", Name: "lookup", Content: resultParts}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP approval response", schema.NewContentBlock(&schema.MCPToolApprovalResponse{ApprovalRequestID: "approval", Approve: true}), convert.AgenticBlockContext{BlockID: "block", ExpectedApprovalRequestID: "approval"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block := *tc.block
			block.StreamingMeta = &schema.StreamingMeta{Index: 0}
			message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{&block}}
			event := adk.EventFromAgenticMessage(nil, schema.StreamReaderFromArray([]*schema.AgenticMessage{message}), schema.AgenticRoleTypeUser)
			resolver := testEventResolverFunc(func(AgentEventCoordinates) (AgentEventResolution, error) {
				return AgentEventResolution{Identity: agenticIDs(), Blocks: testBlockResolver{0: tc.context}}, nil
			})
			result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), resolver)
			if err != nil || len(result.Projections) != 1 || len(result.Projections[0].Public.ContentBlocks) != 1 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if got := result.Projections[0].Public.ContentBlocks[0].Type; got != block.Type {
				t.Fatalf("type = %q, want %q", got, block.Type)
			}
		})
	}
}

func TestDrainAgenticEventsProjectsInterruptWithoutPrivateInfo(t *testing.T) {
	t.Parallel()
	event := &adk.TypedAgentEvent[*schema.AgenticMessage]{Action: &adk.AgentAction{Interrupted: &adk.InterruptInfo{
		Data:              "PRIVATE_CHECKPOINT",
		InterruptContexts: []*adk.InterruptCtx{{ID: "interrupt-1", Address: adk.Address{{Type: adk.AddressSegmentAgent, ID: "root"}}, Info: "PRIVATE_INFO"}},
	}}}
	resolver := testEventResolverFunc(func(AgentEventCoordinates) (AgentEventResolution, error) {
		return AgentEventResolution{
			Identity: agenticIDs(), Blocks: testBlockResolver{}, PauseID: "pause-1",
			Correlation: &convert.ApprovalInterruptCorrelation{ApprovalRequestID: "approval-1", InterruptTargetID: "interrupt-1", InterruptAddress: "agent:root"},
		}, nil
	})
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Interrupts) != 1 || len(result.Interrupts[0].Targets) != 1 {
		t.Fatalf("interrupts = %#v", result.Interrupts)
	}
	if got := result.Interrupts[0].Targets[0].Address; got != "agent:root" {
		t.Fatalf("address = %q", got)
	}
	if result.Interrupts[0].PauseID != "pause-1" || result.Interrupts[0].Correlation == nil || result.Interrupts[0].Correlation.ApprovalRequestID != "approval-1" {
		t.Fatalf("interrupt = %#v", result.Interrupts[0])
	}
}

func TestDrainAgenticEventsClassifiesCancellation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		info           *adk.AgentCancelInfo
		requested      string
		observed       string
		classification string
	}{
		{"immediate", &adk.AgentCancelInfo{Mode: adk.CancelImmediate}, "immediate", "immediate", "immediate"},
		{"chat model safe point", &adk.AgentCancelInfo{Mode: adk.CancelAfterChatModel}, "after_chat_model", "after_chat_model", "safe_point"},
		{"tool safe point", &adk.AgentCancelInfo{Mode: adk.CancelAfterToolCalls}, "after_tool_calls", "after_tool_calls", "safe_point"},
		{"graceful escalation", &adk.AgentCancelInfo{Mode: adk.CancelAfterChatModel, Escalated: true}, "after_chat_model", "immediate", "escalated"},
		{"timeout escalation", &adk.AgentCancelInfo{Mode: adk.CancelAfterToolCalls, Escalated: true, Timeout: true}, "after_tool_calls", "immediate", "timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &adk.TypedAgentEvent[*schema.AgenticMessage]{Err: &adk.CancelError{Info: tc.info}}
			result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), testEventResolver{})
			if err != nil {
				t.Fatalf("cancellation should be an observation: %v", err)
			}
			if len(result.Cancellations) != 1 {
				t.Fatalf("cancellations = %#v", result.Cancellations)
			}
			got := result.Cancellations[0].Cancellation
			if got.RequestedMode != tc.requested || got.ObservedMode != tc.observed || got.Classification != tc.classification {
				t.Fatalf("cancellation = %#v", got)
			}
			if !sameStreamIdentity(result.Cancellations[0].Identity, agenticIDs()) {
				t.Fatalf("cancellation identity = %#v", result.Cancellations[0].Identity)
			}
		})
	}
}

func TestAgentEventSourceCancellationAbortsAndJoinsOnce(t *testing.T) {
	t.Parallel()
	iterator, generator := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	var aborts atomic.Int32
	var waits atomic.Int32
	source, err := NewAgentEventSource(iterator, func(error) { aborts.Add(1); generator.Close() }, func(context.Context) error { waits.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancellation := convert.CancelledV1{RequestedMode: "immediate", ObservedMode: "immediate", Classification: "immediate"}
	result, err := DrainAgenticEvents(ctx, source, testEventResolver{}, WithAgentEventCleanupDeadline(time.Second), WithAgentEventCancellationCandidate(agenticIDs(), cancellation))
	if !errors.Is(err, context.Canceled) || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if aborts.Load() != 1 {
		t.Fatalf("aborts = %d", aborts.Load())
	}
	if waits.Load() != 1 {
		t.Fatalf("waits = %d", waits.Load())
	}
	if len(result.Cancellations) != 1 || !sameStreamIdentity(result.Cancellations[0].Identity, agenticIDs()) || result.Cancellations[0].Cancellation != cancellation {
		t.Fatalf("cancellations = %#v", result.Cancellations)
	}
}

func TestDrainAgenticEventsReportsCleanupContractFailure(t *testing.T) {
	t.Parallel()
	iterator, generator := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	generator.Close()
	waitErr := errors.New("producer did not join")
	source, err := NewAgentEventSource(iterator, func(error) {}, func(context.Context) error { return waitErr })
	if err != nil {
		t.Fatal(err)
	}
	result, err := DrainAgenticEvents(t.Context(), source, testEventResolver{})
	var cleanup *CleanupContractError
	if !errors.As(err, &cleanup) || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDrainAgenticEventsRejectsInvalidMessageUnion(t *testing.T) {
	t.Parallel()
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}
	event := adk.EventFromAgenticMessage(message, schema.StreamReaderFromArray([]*schema.AgenticMessage{message}), schema.AgenticRoleTypeAssistant)
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), testEventResolver{})
	if err == nil || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDrainAgenticEventsReturnsOnlyExplicitSubagentLifecycle(t *testing.T) {
	t.Parallel()
	event := &adk.TypedAgentEvent[*schema.AgenticMessage]{AgentName: "research"}
	ids := agenticIDs()
	ids.AgentPath = append(ids.AgentPath, convert.AgentPathSegment{Name: "research", RunID: "sub-run"})
	resolver := testEventResolverFunc(func(AgentEventCoordinates) (AgentEventResolution, error) {
		return AgentEventResolution{
			Identity: ids, Blocks: testBlockResolver{}, ParentRunID: "run", SubagentRunID: "sub-run",
			Subagent: &SubagentLifecycleResolution{Kind: SubagentLifecycleFinished, Detail: "committed candidate"},
		}, nil
	})
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subagents) != 1 || result.Subagents[0].Kind != SubagentLifecycleFinished || result.Subagents[0].SubagentRunID != "sub-run" {
		t.Fatalf("subagents = %#v", result.Subagents)
	}

	plain, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), testEventResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Subagents) != 0 {
		t.Fatalf("inferred lifecycle = %#v", plain.Subagents)
	}
}

func TestDrainAgenticEventsKeepsRepeatedSiblingNamesDistinctByHostRunID(t *testing.T) {
	t.Parallel()
	events := []*adk.TypedAgentEvent[*schema.AgenticMessage]{
		{AgentName: "root"},
		{AgentName: "research"},
		{AgentName: "research"},
	}
	resolver := testEventResolverFunc(func(coordinates AgentEventCoordinates) (AgentEventResolution, error) {
		identity := agenticIDs()
		if coordinates.EventOrdinal == 0 {
			return AgentEventResolution{Identity: identity, Blocks: testBlockResolver{}}, nil
		}
		subagentRunID := "sub-run-one"
		if coordinates.EventOrdinal == 2 {
			subagentRunID = "sub-run-two"
		}
		identity.AgentPath = append(identity.AgentPath, convert.AgentPathSegment{Name: "research", RunID: subagentRunID})
		return AgentEventResolution{
			Identity: identity, Blocks: testBlockResolver{}, ParentRunID: "run", SubagentRunID: subagentRunID,
			Subagent: &SubagentLifecycleResolution{Kind: SubagentLifecycleFinished},
		}, nil
	})
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, events...), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subagents) != 2 || result.Subagents[0].AgentName != "research" || result.Subagents[1].AgentName != "research" || result.Subagents[0].SubagentRunID == result.Subagents[1].SubagentRunID {
		t.Fatalf("subagents = %#v", result.Subagents)
	}
	if result.Subagents[0].Identity.AgentPath[1].RunID != "sub-run-one" || result.Subagents[1].Identity.AgentPath[1].RunID != "sub-run-two" {
		t.Fatalf("subagent paths = %#v / %#v", result.Subagents[0].Identity.AgentPath, result.Subagents[1].Identity.AgentPath)
	}
}

type cleanupFailureSource struct{}

func (cleanupFailureSource) Next(ctx context.Context) (*adk.TypedAgentEvent[*schema.AgenticMessage], bool, error) {
	<-ctx.Done()
	return nil, false, ctx.Err()
}
func (cleanupFailureSource) Abort(error) {}
func (cleanupFailureSource) Wait(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestDrainAgenticEventsBlockedProducerReportsCleanupContract(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := DrainAgenticEvents(ctx, cleanupFailureSource{}, testEventResolver{}, WithAgentEventCleanupDeadline(time.Millisecond))
	var cleanup *CleanupContractError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &cleanup) || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDrainAgenticEventsSourceErrorAfterOutputAbortsAndWaitsOnce(t *testing.T) {
	t.Parallel()
	sourceErr := errors.New("iterator failed")
	message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
		schema.NewContentBlock(&schema.AssistantGenText{Text: "complete"}),
	}}
	source := &scriptedAgentEventSource{
		events:      []*adk.TypedAgentEvent[*schema.AgenticMessage]{adk.EventFromAgenticMessage(message, nil, schema.AgenticRoleTypeAssistant)},
		terminalErr: sourceErr,
	}
	result, err := DrainAgenticEvents(t.Context(), source, testEventResolver{})
	if !errors.Is(err, sourceErr) || result == nil || !result.Partial || len(result.Projections) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if source.aborts.Load() != 1 || source.waits.Load() != 1 {
		t.Fatalf("aborts=%d waits=%d", source.aborts.Load(), source.waits.Load())
	}
}

func TestDrainAgenticEventsReaderErrorClosesReaderAndCleansSourceOnce(t *testing.T) {
	t.Parallel()
	readerErr := errors.New("reader failed")
	reader, writer := schema.Pipe[*schema.AgenticMessage](2)
	chunk := testmodel.AgenticTextChunks(0, "partial")[0]
	if writer.Send(chunk, nil) || writer.Send(nil, readerErr) {
		t.Fatal("reader closed before drain")
	}
	writer.Close()
	event := adk.EventFromAgenticMessage(nil, reader, schema.AgenticRoleTypeAssistant)
	source := &scriptedAgentEventSource{events: []*adk.TypedAgentEvent[*schema.AgenticMessage]{event}}
	result, err := DrainAgenticEvents(t.Context(), source, testEventResolver{})
	if !errors.Is(err, readerErr) || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if source.aborts.Load() != 1 || source.waits.Load() != 1 {
		t.Fatalf("aborts=%d waits=%d", source.aborts.Load(), source.waits.Load())
	}
	if !writer.Send(chunk, nil) {
		t.Fatal("nested reader was not closed")
	}
}

func TestDrainAgenticEventsClosesUndrainedReaderWhenEventHasError(t *testing.T) {
	t.Parallel()
	reader, writer := schema.Pipe[*schema.AgenticMessage](1)
	event := adk.EventFromAgenticMessage(nil, reader, schema.AgenticRoleTypeAssistant)
	eventErr := errors.New("event failed before stream drain")
	event.Err = eventErr
	source := &scriptedAgentEventSource{events: []*adk.TypedAgentEvent[*schema.AgenticMessage]{event}}
	result, err := DrainAgenticEvents(t.Context(), source, testEventResolver{})
	if !errors.Is(err, eventErr) || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if source.aborts.Load() != 1 || source.waits.Load() != 1 {
		t.Fatalf("aborts=%d waits=%d", source.aborts.Load(), source.waits.Load())
	}
	if !writer.Send(testmodel.AgenticTextChunks(0, "late")[0], nil) {
		t.Fatal("undrained event reader was not closed")
	}
}

func TestDrainAgenticEventsObserverFailureDoesNotAbortBlockedSourceAndProjectionReplays(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	messageStream := schema.StreamReaderFromArray(testmodel.AgenticTextChunks(0, "complete"))
	event := adk.EventFromAgenticMessage(nil, messageStream, schema.AgenticRoleTypeAssistant)
	source := &scriptedAgentEventSource{events: []*adk.TypedAgentEvent[*schema.AgenticMessage]{event}, release: release}
	transport := &notifyingErrorWriter{failed: make(chan struct{})}
	observer := emitter.NewObserverSink(emitter.NewObserverEmitter(t.Context(), bufio.NewWriter(transport), sse.NewSSEWriter()))
	type drainResult struct {
		result *AgentEventResult
		err    error
	}
	done := make(chan drainResult, 1)
	go func() {
		result, err := DrainAgenticEvents(t.Context(), source, testEventResolver{}, WithAgentEventTransientSink(observer))
		done <- drainResult{result: result, err: err}
	}()

	select {
	case <-transport.failed:
	case <-time.After(time.Second):
		t.Fatal("observer transport did not fail")
	}
	if source.aborts.Load() != 0 {
		t.Fatalf("observer failure aborted execution %d times", source.aborts.Load())
	}
	close(release)
	drained := <-done
	if drained.err != nil || drained.result == nil || drained.result.Partial || len(drained.result.Projections) != 1 || drained.result.ObserverErr == nil {
		t.Fatalf("result=%#v err=%v", drained.result, drained.err)
	}
	if source.aborts.Load() != 0 || source.waits.Load() != 1 || observer.Err() == nil {
		t.Fatalf("aborts=%d waits=%d observer error=%v", source.aborts.Load(), source.waits.Load(), observer.Err())
	}

	projection := drained.result.Projections[0]
	replaySink := testsse.NewSink()
	replayEmitter := emitter.NewObserverEmitter(t.Context(), replaySink.Writer(), replaySink.SSEWriter())
	receipt := convert.CommitReceiptV1{Revision: "revision", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
	if !replayEmitter.EmitCommittedProjection(projection, receipt, emitter.DeliveryModeReplay) {
		t.Fatalf("replay failed: %v / %v", replayEmitter.Err(), replayEmitter.EncErr())
	}
	if err := replaySink.Flush(); err != nil || len(replaySink.Frames()) == 0 {
		t.Fatalf("replay frames=%d err=%v", len(replaySink.Frames()), err)
	}
}

func TestDrainAgenticEventsRejectsEmptyTransferDestination(t *testing.T) {
	t.Parallel()
	event := &adk.TypedAgentEvent[*schema.AgenticMessage]{Action: adk.NewTransferToAgentAction("")}
	source := &scriptedAgentEventSource{events: []*adk.TypedAgentEvent[*schema.AgenticMessage]{event}}
	result, err := DrainAgenticEvents(t.Context(), source, testEventResolver{})
	if err == nil || result == nil || !result.Partial || len(result.Controls) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if source.aborts.Load() != 1 || source.waits.Load() != 1 {
		t.Fatalf("aborts=%d waits=%d", source.aborts.Load(), source.waits.Load())
	}
}

func TestDrainAgenticEventsReturnsHostControlObservationsInOrder(t *testing.T) {
	t.Parallel()
	events := []*adk.TypedAgentEvent[*schema.AgenticMessage]{
		{Action: adk.NewTransferToAgentAction("research")},
		{Action: adk.NewExitAction()},
		{Action: adk.NewBreakLoopAction("root")},
	}
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, events...), testEventResolver{})
	if err != nil {
		t.Fatal(err)
	}
	want := []AgentControlObservation{
		{Kind: AgentControlTransfer, Destination: "research"},
		{Kind: AgentControlExit},
		{Kind: AgentControlBreakLoop},
	}
	if result.Partial || !reflect.DeepEqual(result.Controls, want) {
		t.Fatalf("result=%#v controls=%#v, want %#v", result, result.Controls, want)
	}
	if len(result.Cancellations) != 0 || len(result.Interrupts) != 0 || len(result.Subagents) != 0 {
		t.Fatalf("control actions produced lifecycle candidates: %#v", result)
	}
}
