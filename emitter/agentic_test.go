package emitter

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/internal/golden"
	"github.com/mattsp1290/eino-agui/internal/testsse"
)

func agenticProjection(t *testing.T) *convert.AgenticProjection {
	return agenticProjectionForAttempt(t, "attempt")
}

func agenticProjectionForAttempt(t *testing.T, attemptID string) *convert.AgenticProjection {
	t.Helper()
	id := convert.AgenticIdentityV1{
		SessionID: "session", ThreadID: "session", RunID: "run", TurnID: "turn",
		MessageID: "message", AttemptID: attemptID,
		AgentPath: []convert.AgentPathSegment{{Name: "root", RunID: "run"}},
	}
	projection, err := convert.ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "hello"}),
			schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call", Name: "lookup", Arguments: `{}`}),
		}},
		convert.AgenticProjectionContext{Identity: id, Blocks: []convert.AgenticBlockContext{{BlockID: "text"}, {BlockID: "tool"}}, Limits: convert.DefaultProjectionLimits()},
	)
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func TestAttemptReplacementMustPrecedeSuccessorOutput(t *testing.T) {
	t.Parallel()
	oldProjection := agenticProjectionForAttempt(t, "attempt-old")
	newProjection := agenticProjectionForAttempt(t, "attempt-new")
	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	receiptFor := func(projection *convert.AgenticProjection) convert.CommitReceiptV1 {
		return convert.CommitReceiptV1{Revision: "revision", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
	}
	if !emit.EmitCommittedProjection(oldProjection, receiptFor(oldProjection), DeliveryModeLiveContinuation) {
		t.Fatalf("old attempt emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
	wantPrefix := len(normalizedFrames(t, sink))
	if emit.EmitCommittedProjection(newProjection, receiptFor(newProjection), DeliveryModeLiveContinuation) {
		t.Fatal("successor output emitted before replacement")
	}
	if got := len(normalizedFrames(t, sink)); got != wantPrefix {
		t.Fatalf("successor rejection wrote %d frames, want %d", got, wantPrefix)
	}

	replacement := convert.AttemptReplacedV1{OldAttemptID: "attempt-old", NewAttemptID: "attempt-new", Cause: "retry", Semantics: "replace"}
	envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: convert.EnvelopeAttemptReplaced, Identity: oldProjection.Public.Identity, AttemptReplaced: &replacement}
	digest, err := convert.LifecycleDigestV1(envelope)
	if err != nil {
		t.Fatal(err)
	}
	receipt := convert.CommitReceiptV1{Revision: "revision-2", Domain: "lifecycle", Kind: envelope.Kind, Identity: envelope.Identity, Digest: digest}
	if !emit.AttemptReplaced(replacement, receipt) {
		t.Fatalf("replacement emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
	if !emit.EmitCommittedProjection(newProjection, receiptFor(newProjection), DeliveryModeLiveContinuation) {
		t.Fatalf("successor emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
}

func TestAgenticSSEPassesPinnedStrictDecoder(t *testing.T) {
	t.Parallel()
	projection := agenticProjection(t)
	sink := testsse.NewSink()
	emit := NewObserverEmitter(context.Background(), sink.Writer(), sink.SSEWriter())
	receipt := convert.CommitReceiptV1{Revision: "rev-1", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
	if !emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
		t.Fatalf("emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	decoder := events.NewEventDecoder(nil)
	for _, frame := range bytes.Split(sink.Bytes(), []byte("\n\n")) {
		if len(bytes.TrimSpace(frame)) == 0 {
			continue
		}
		var eventType string
		var data []byte
		for _, line := range bytes.Split(frame, []byte("\n")) {
			switch {
			case bytes.HasPrefix(line, []byte("event: ")):
				eventType = strings.TrimPrefix(string(line), "event: ")
			case bytes.HasPrefix(line, []byte("data: ")):
				data = append([]byte(nil), bytes.TrimPrefix(line, []byte("data: "))...)
			}
		}
		if eventType == "" {
			var header struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &header); err != nil {
				t.Fatal(err)
			}
			eventType = header.Type
		}
		decoded, err := decoder.DecodeEvent(eventType, data)
		if err != nil {
			t.Fatalf("strict decode %s: %v\n%s", eventType, err, data)
		}
		if err := decoded.Validate(); err != nil {
			t.Fatalf("strict validate %s: %v", eventType, err)
		}
		if eventType == "CUSTOM" {
			custom := decoded.(*events.CustomEvent)
			encoded, err := json.Marshal(custom.Value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := convert.DecodeAgenticEnvelope(encoded); err != nil {
				t.Fatalf("agentic decode: %v", err)
			}
		}
	}
}

func TestEmitCommittedProjectionModes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mode DeliveryMode
		want []string
	}{
		{"live continuation", DeliveryModeLiveContinuation, []string{"CUSTOM", "CUSTOM"}},
		{"committed only", DeliveryModeCommittedOnly, []string{"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "CUSTOM", "TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "CUSTOM"}},
		{"replay", DeliveryModeReplay, []string{"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "CUSTOM", "TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "CUSTOM"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projection := agenticProjection(t)
			sink := testsse.NewSink()
			emit := NewObserverEmitter(context.Background(), sink.Writer(), sink.SSEWriter())
			receipt := convert.CommitReceiptV1{Revision: "rev-1", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
			if !emit.EmitCommittedProjection(projection, receipt, tc.mode) {
				t.Fatalf("emit failed: %v / %v", emit.Err(), emit.EncErr())
			}
			frames := normalizedFrames(t, sink)
			if got := golden.FrameTypes(frames); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("types = %v, want %v", got, tc.want)
			}
			for _, frame := range frames {
				metadata, ok := frame.Data["metadata"].(map[string]any)
				if !ok || metadata[convert.AgenticCustomEventName] == nil {
					t.Fatalf("missing agentic identity: %#v", frame.Data)
				}
			}
		})
	}
}

func TestEmitTransientBlockUsesChunkAndDoesNotAuthorizeCommit(t *testing.T) {
	t.Parallel()
	projection := agenticProjection(t)
	block, err := convert.TransientEventForBlock(projection.Blocks[0].Public)
	if err != nil {
		t.Fatal(err)
	}
	sink := testsse.NewSink()
	emit := NewObserverEmitter(context.Background(), sink.Writer(), sink.SSEWriter())
	if !emit.EmitTransientBlock(block) {
		t.Fatalf("transient emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
	frames := normalizedFrames(t, sink)
	if got := golden.FrameTypes(frames); !reflect.DeepEqual(got, []string{"TEXT_MESSAGE_CHUNK"}) {
		t.Fatalf("types = %v", got)
	}
	identity := frames[0].Data["metadata"].(map[string]any)[convert.AgenticCustomEventName].(map[string]any)
	if identity["transient"] != true {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestCommittedProjectionRejectsMismatchedReceiptWithoutBytes(t *testing.T) {
	t.Parallel()
	projection := agenticProjection(t)
	sink := testsse.NewSink()
	emit := NewObserverEmitter(context.Background(), sink.Writer(), sink.SSEWriter())
	receipt := convert.CommitReceiptV1{Revision: "rev-1", Domain: "projection", Identity: projection.Public.Identity, Digest: "wrong"}
	if emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
		t.Fatal("mismatched receipt emitted")
	}
	if got := normalizedFrames(t, sink); len(got) != 0 {
		t.Fatalf("frames = %#v, want none", got)
	}
}

func TestCommittedLifecycleRejectsFailedCommitWithoutBytes(t *testing.T) {
	t.Parallel()
	root := agenticProjection(t).Public.Identity
	nested := root
	nested.AgentPath = append(append([]convert.AgentPathSegment(nil), root.AgentPath...), convert.AgentPathSegment{Name: "research", RunID: "sub-run"})
	tests := []struct {
		name string
		call func(*Emitter) bool
	}{
		{"pause", func(e *Emitter) bool {
			return e.Paused(convert.PausedV1{Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}}, convert.CommitReceiptV1{Revision: "failed", Domain: "lifecycle", Kind: convert.EnvelopePaused, Identity: root, Digest: "not-committed"})
		}},
		{"cancellation", func(e *Emitter) bool {
			return e.Cancelled(convert.CancelledV1{RequestedMode: "immediate", ObservedMode: "immediate", Classification: "immediate"}, convert.CommitReceiptV1{Revision: "failed", Domain: "lifecycle", Kind: convert.EnvelopeCancelled, Identity: root, Digest: "not-committed"})
		}},
		{"subagent completion", func(e *Emitter) bool {
			return e.SubagentFinishedCommitted(convert.LifecycleFactV1{}, convert.CommitReceiptV1{Revision: "failed", Domain: "lifecycle", Kind: convert.EnvelopeSubagentFinished, Identity: nested, Digest: "not-committed"})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := testsse.NewSink()
			emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
			if tc.call(emit) {
				t.Fatal("failed commit emitted")
			}
			if got := normalizedFrames(t, sink); len(got) != 0 {
				t.Fatalf("frames = %#v, want none", got)
			}
		})
	}
}

func TestCommittedProjectionPrevalidatesEveryBlockBeforeBytes(t *testing.T) {
	t.Parallel()
	projection := agenticProjection(t)
	projection.Blocks[1].Public.Identity.BlockID = "tampered"
	sink := testsse.NewSink()
	emit := NewObserverEmitter(context.Background(), sink.Writer(), sink.SSEWriter())
	receipt := convert.CommitReceiptV1{Revision: "rev-1", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
	if emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
		t.Fatal("tampered later block emitted")
	}
	if got := normalizedFrames(t, sink); len(got) != 0 {
		t.Fatalf("frames = %#v, want none", got)
	}
}

func TestTurnFinishedIsCustomOnlyAndRunTerminalRequiresSettlement(t *testing.T) {
	t.Parallel()
	id := agenticProjection(t).Public.Identity
	turn := convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeTurnFinished, Identity: id, Lifecycle: &convert.LifecycleFactV1{Detail: "done"}}
	digest, err := convert.LifecycleDigestV1(&turn)
	if err != nil {
		t.Fatal(err)
	}
	sink := testsse.NewSink()
	emit := NewObserverEmitter(context.Background(), sink.Writer(), sink.SSEWriter())
	if !emit.TurnFinished(*turn.Lifecycle, convert.CommitReceiptV1{Revision: "rev", Domain: "lifecycle", Kind: turn.Kind, Identity: id, Digest: digest}) {
		t.Fatalf("turn finish failed: %v", emit.EncErr())
	}
	if emit.RunFinishedCommitted(convert.LifecycleFactV1{}, convert.CommitReceiptV1{}) {
		t.Fatal("unsettled run finish emitted")
	}
	if got := golden.FrameTypes(normalizedFrames(t, sink)); !reflect.DeepEqual(got, []string{"CUSTOM"}) {
		t.Fatalf("types = %v", got)
	}
}

func TestCommittedLifecycleWrappers(t *testing.T) {
	t.Parallel()
	root := agenticProjection(t).Public.Identity
	nested := root
	nested.AgentPath = append(append([]convert.AgentPathSegment(nil), root.AgentPath...), convert.AgentPathSegment{Name: "research", RunID: "sub-run"})

	tests := []struct {
		name     string
		kind     convert.AgenticEnvelopeKind
		identity convert.AgenticIdentityV1
		build    func(*convert.AgenticEnvelopeV1)
		emit     func(*Emitter, convert.CommitReceiptV1) bool
		want     []string
	}{
		{"run started", convert.EnvelopeRunStarted, root, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &convert.LifecycleFactV1{} }, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.RunStartedCommitted(convert.LifecycleFactV1{}, r)
		}, []string{"RUN_STARTED", "CUSTOM"}},
		{"turn started", convert.EnvelopeTurnStarted, root, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &convert.LifecycleFactV1{} }, func(e *Emitter, r convert.CommitReceiptV1) bool { return e.TurnStarted(convert.LifecycleFactV1{}, r) }, []string{"CUSTOM"}},
		{"turn finished", convert.EnvelopeTurnFinished, root, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &convert.LifecycleFactV1{Detail: "done"} }, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.TurnFinished(convert.LifecycleFactV1{Detail: "done"}, r)
		}, []string{"CUSTOM"}},
		{"attempt replaced", convert.EnvelopeAttemptReplaced, root, func(v *convert.AgenticEnvelopeV1) {
			v.AttemptReplaced = &convert.AttemptReplacedV1{OldAttemptID: "old", NewAttemptID: "new", Cause: "retry", Semantics: "replace"}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.AttemptReplaced(convert.AttemptReplacedV1{OldAttemptID: "old", NewAttemptID: "new", Cause: "retry", Semantics: "replace"}, r)
		}, []string{"CUSTOM"}},
		{"paused", convert.EnvelopePaused, root, func(v *convert.AgenticEnvelopeV1) {
			v.Paused = &convert.PausedV1{Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.Paused(convert.PausedV1{Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}}, r)
		}, []string{"CUSTOM"}},
		{"resumed", convert.EnvelopeResumed, root, func(v *convert.AgenticEnvelopeV1) {
			v.Resumed = &convert.ResumedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}, Full: true, NewTurnID: "turn-2", NewAttemptID: "attempt-2"}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.Resumed(convert.ResumedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}, Full: true, NewTurnID: "turn-2", NewAttemptID: "attempt-2"}, r)
		}, []string{"CUSTOM"}},
		{"cancelled", convert.EnvelopeCancelled, root, func(v *convert.AgenticEnvelopeV1) {
			v.Cancelled = &convert.CancelledV1{RequestedMode: "graceful", ObservedMode: "immediate", Classification: "timeout"}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.Cancelled(convert.CancelledV1{RequestedMode: "graceful", ObservedMode: "immediate", Classification: "timeout"}, r)
		}, []string{"CUSTOM"}},
		{"subagent started", convert.EnvelopeSubagentStarted, nested, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &convert.LifecycleFactV1{} }, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.SubagentStartedCommitted(convert.LifecycleFactV1{}, r)
		}, []string{"SUBAGENT_STARTED", "CUSTOM"}},
		{"subagent finished", convert.EnvelopeSubagentFinished, nested, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &convert.LifecycleFactV1{} }, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.SubagentFinishedCommitted(convert.LifecycleFactV1{}, r)
		}, []string{"SUBAGENT_FINISHED", "CUSTOM"}},
		{"subagent error", convert.EnvelopeSubagentError, nested, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &convert.LifecycleFactV1{Detail: "failed"} }, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.SubagentErrorCommitted(convert.LifecycleFactV1{Detail: "failed"}, r)
		}, []string{"SUBAGENT_ERROR", "CUSTOM"}},
		{"run finished", convert.EnvelopeRunFinished, root, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &convert.LifecycleFactV1{LoopSettled: true} }, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.RunFinishedCommitted(convert.LifecycleFactV1{LoopSettled: true}, r)
		}, []string{"RUN_FINISHED", "CUSTOM"}},
		{"run error", convert.EnvelopeRunError, root, func(v *convert.AgenticEnvelopeV1) {
			v.Lifecycle = &convert.LifecycleFactV1{Detail: "failed", LoopSettled: true}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.RunErrorCommitted(convert.LifecycleFactV1{Detail: "failed", LoopSettled: true}, r)
		}, []string{"RUN_ERROR", "CUSTOM"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: tc.kind, Identity: tc.identity}
			tc.build(envelope)
			digest, err := convert.LifecycleDigestV1(envelope)
			if err != nil {
				t.Fatal(err)
			}
			sink := testsse.NewSink()
			emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
			receipt := convert.CommitReceiptV1{Revision: "revision", Domain: "lifecycle", Kind: tc.kind, Identity: tc.identity, Digest: digest}
			if !tc.emit(emit, receipt) {
				t.Fatalf("emit failed: %v / %v", emit.Err(), emit.EncErr())
			}
			if got := golden.FrameTypes(normalizedFrames(t, sink)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("types = %v, want %v", got, tc.want)
			}
		})
	}
}
