package stream

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/internal/testmodel"
)

type testEventResolver struct{}

func (testEventResolver) ResolveAgentEvent(AgentEventCoordinates) (AgentEventResolution, error) {
	return AgentEventResolution{Identity: agenticIDs(), Blocks: testBlockResolver{0: {BlockID: "block"}}}, nil
}

type testEventResolverFunc func(AgentEventCoordinates) (AgentEventResolution, error)

func (f testEventResolverFunc) ResolveAgentEvent(coordinates AgentEventCoordinates) (AgentEventResolution, error) {
	return f(coordinates)
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

func TestDrainAgenticEventsProjectsInterruptWithoutPrivateInfo(t *testing.T) {
	t.Parallel()
	event := &adk.TypedAgentEvent[*schema.AgenticMessage]{Action: &adk.AgentAction{Interrupted: &adk.InterruptInfo{
		Data:              "PRIVATE_CHECKPOINT",
		InterruptContexts: []*adk.InterruptCtx{{ID: "interrupt-1", Address: adk.Address{{Type: adk.AddressSegmentAgent, ID: "root"}}, Info: "PRIVATE_INFO"}},
	}}}
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), testEventResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Interrupts) != 1 || len(result.Interrupts[0].Targets) != 1 {
		t.Fatalf("interrupts = %#v", result.Interrupts)
	}
	if got := result.Interrupts[0].Targets[0].Address; got != "agent:root" {
		t.Fatalf("address = %q", got)
	}
}

func TestDrainAgenticEventsClassifiesCancellation(t *testing.T) {
	t.Parallel()
	event := &adk.TypedAgentEvent[*schema.AgenticMessage]{Err: &adk.CancelError{Info: &adk.AgentCancelInfo{Mode: adk.CancelImmediate}}}
	result, err := DrainAgenticEvents(t.Context(), sourceFromEvents(t, event), testEventResolver{})
	if err != nil {
		t.Fatalf("cancellation should be an observation: %v", err)
	}
	if len(result.Cancellations) != 1 || result.Cancellations[0].Classification != "immediate" {
		t.Fatalf("cancellations = %#v", result.Cancellations)
	}
}

func TestAgentEventSourceCancellationAbortsAndJoinsOnce(t *testing.T) {
	t.Parallel()
	iterator, generator := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	var aborts atomic.Int32
	source, err := NewAgentEventSource(iterator, func(error) { aborts.Add(1); generator.Close() }, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := DrainAgenticEvents(ctx, source, testEventResolver{}, WithAgentEventCleanupDeadline(time.Second))
	if !errors.Is(err, context.Canceled) || result == nil || !result.Partial {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if aborts.Load() != 1 {
		t.Fatalf("aborts = %d", aborts.Load())
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
