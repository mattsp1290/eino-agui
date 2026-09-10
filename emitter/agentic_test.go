package emitter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/internal/golden"
	"github.com/mattsp1290/eino-agui/internal/testsse"
)

var _ interface {
	Emit(events.Event) error
	Detach(error)
} = (*ObserverSink)(nil)

type failAfterEventWrites struct {
	buffer  bytes.Buffer
	allowed int
	writes  int
}

func (w *failAfterEventWrites) Write(data []byte) (int, error) {
	if w.writes >= w.allowed {
		return 0, errors.New("broken pipe")
	}
	w.writes++
	return w.buffer.Write(data)
}

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
	strictDecodeAgenticSSE(t, sink.Bytes())
}

func strictDecodeAgenticSSE(t *testing.T, data []byte) []events.Event {
	t.Helper()
	decoder := events.NewEventDecoder(nil)
	var decodedEvents []events.Event
	for _, frame := range bytes.Split(data, []byte("\n\n")) {
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
		decodedEvents = append(decodedEvents, decoded)
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
	return decodedEvents
}

func TestAllAgenticContentKindsPassPinnedWireDecoders(t *testing.T) {
	t.Parallel()
	code := int64(7)
	resultParts := []*schema.FunctionToolResultContentBlock{
		{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "text"}},
		{Type: schema.FunctionToolResultContentBlockTypeImage, Image: &schema.UserInputImage{URL: "https://example.test/image"}},
		{Type: schema.FunctionToolResultContentBlockTypeAudio, Audio: &schema.UserInputAudio{URL: "https://example.test/audio"}},
		{Type: schema.FunctionToolResultContentBlockTypeVideo, Video: &schema.UserInputVideo{URL: "https://example.test/video"}},
		{Type: schema.FunctionToolResultContentBlockTypeFile, File: &schema.UserInputFile{URL: "https://example.test/file", Name: "file.pdf"}},
	}
	tests := []struct {
		name    string
		role    schema.AgenticRoleType
		block   *schema.ContentBlock
		context convert.AgenticBlockContext
	}{
		{"reasoning", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.Reasoning{Text: "why"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user text", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputText{Text: "hello"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user image", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputImage{URL: "https://example.test/image"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user audio", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputAudio{Base64Data: "YWJj", MIMEType: "audio/wav"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user video", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputVideo{URL: "https://example.test/video"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"user file", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.UserInputFile{URL: "https://example.test/file", Name: "file.pdf"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"tool search", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.ToolSearchFunctionToolResult{CallID: "search", Name: "search", Result: &schema.ToolSearchResult{Tools: []*schema.ToolInfo{{Name: "lookup"}}}}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant text", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenText{Text: "answer"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant image", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenImage{URL: "https://example.test/generated-image"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant audio", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenAudio{URL: "https://example.test/generated-audio"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"assistant video", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.AssistantGenVideo{URL: "https://example.test/generated-video"}), convert.AgenticBlockContext{BlockID: "block"}},
		{"function call", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.FunctionToolCall{CallID: "function", Name: "lookup", Arguments: `{}`}), convert.AgenticBlockContext{BlockID: "block"}},
		{"function result", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.FunctionToolResult{CallID: "function", Name: "lookup", Content: resultParts}), convert.AgenticBlockContext{BlockID: "block"}},
		{"server call", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.ServerToolCall{CallID: "server", Name: "search", Arguments: map[string]any{"q": "go"}}), convert.AgenticBlockContext{BlockID: "block", ProviderServerID: "provider"}},
		{"server result", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.ServerToolResult{CallID: "server", Name: "search", Content: []any{"result"}}), convert.AgenticBlockContext{BlockID: "block", ProviderServerID: "provider"}},
		{"MCP call", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPToolCall{ServerLabel: "server", CallID: "mcp", Name: "lookup", Arguments: `{}`}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP result", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPToolResult{ServerLabel: "server", CallID: "mcp", Name: "lookup", Content: `{}`, Error: &schema.MCPToolCallError{Code: &code, Message: "failed"}}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP list", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPListToolsResult{ServerLabel: "server", Tools: []*schema.MCPListToolsItem{{Name: "lookup"}}}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP approval request", schema.AgenticRoleTypeAssistant, schema.NewContentBlock(&schema.MCPToolApprovalRequest{ID: "approval", ServerLabel: "server", Name: "lookup", Arguments: `{}`}), convert.AgenticBlockContext{BlockID: "block"}},
		{"MCP approval response", schema.AgenticRoleTypeUser, schema.NewContentBlock(&schema.MCPToolApprovalResponse{ApprovalRequestID: "approval", Approve: true}), convert.AgenticBlockContext{BlockID: "block", ExpectedApprovalRequestID: "approval"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := agenticProjection(t).Public.Identity
			projection, err := convert.ToAgenticProjection(&schema.AgenticMessage{Role: tc.role, ContentBlocks: []*schema.ContentBlock{tc.block}}, convert.AgenticProjectionContext{Identity: id, Blocks: []convert.AgenticBlockContext{tc.context}, Limits: convert.DefaultProjectionLimits()})
			if err != nil {
				t.Fatal(err)
			}
			receipt := convert.CommitReceiptV1{Revision: "revision", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
			sink := testsse.NewSink()
			emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
			if !emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
				t.Fatalf("emit failed: %v / %v", emit.Err(), emit.EncErr())
			}
			if err := sink.Flush(); err != nil {
				t.Fatal(err)
			}
			decoded := strictDecodeAgenticSSE(t, sink.Bytes())
			custom := 0
			for _, event := range decoded {
				if event.GetBaseEvent().Type() == events.EventTypeCustom {
					custom++
				}
			}
			if custom != 1 {
				t.Fatalf("custom event count = %d, want 1", custom)
			}
		})
	}
}

func TestObserverSinkAdaptsObserverEmitterAndDetaches(t *testing.T) {
	t.Parallel()
	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	observer := NewObserverSink(emit)
	messageID, role, delta := "message", "assistant", "hello"
	if err := observer.Emit(events.NewTextMessageChunkEvent(&messageID, &role, &delta)); err != nil {
		t.Fatal(err)
	}
	detachErr := errors.New("observer disconnected")
	observer.Detach(detachErr)
	if !errors.Is(observer.Err(), detachErr) {
		t.Fatalf("observer error = %v", observer.Err())
	}
	if err := observer.Emit(events.NewTextMessageChunkEvent(&messageID, &role, &delta)); !errors.Is(err, detachErr) {
		t.Fatalf("detached emit error = %v", err)
	}
	if got := golden.FrameTypes(normalizedFrames(t, sink)); !reflect.DeepEqual(got, []string{"TEXT_MESSAGE_CHUNK"}) {
		t.Fatalf("types = %v", got)
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

func TestCommittedMixedBlocksPreserveExactNativeAndCustomOrder(t *testing.T) {
	t.Parallel()
	id := agenticProjection(t).Public.Identity
	assistant, err := convert.ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.Reasoning{Text: "reason"}),
			schema.NewContentBlock(&schema.AssistantGenText{Text: "answer"}),
			schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call", Name: "lookup", Arguments: `{}`}),
			schema.NewContentBlock(&schema.AssistantGenImage{URL: "https://example.test/image"}),
		}},
		convert.AgenticProjectionContext{Identity: id, Blocks: []convert.AgenticBlockContext{{BlockID: "reason"}, {BlockID: "text"}, {BlockID: "call"}, {BlockID: "image"}}, Limits: convert.DefaultProjectionLimits()},
	)
	if err != nil {
		t.Fatal(err)
	}
	resultID := id
	resultID.MessageID = "result-message"
	result, err := convert.ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolResult{
			CallID: "call", Name: "lookup", Content: []*schema.FunctionToolResultContentBlock{{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "result"}}},
		})}},
		convert.AgenticProjectionContext{Identity: resultID, Blocks: []convert.AgenticBlockContext{{BlockID: "result"}}, Limits: convert.DefaultProjectionLimits()},
	)
	if err != nil {
		t.Fatal(err)
	}
	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	for i, projection := range []*convert.AgenticProjection{assistant, result} {
		receipt := convert.CommitReceiptV1{Revision: fmt.Sprintf("revision-%d", i), Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
		if !emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
			t.Fatalf("emit %d failed: %v / %v", i, emit.Err(), emit.EncErr())
		}
	}
	want := []string{
		"REASONING_START", "REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT", "REASONING_MESSAGE_END", "REASONING_END", "CUSTOM",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "CUSTOM",
		"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "CUSTOM",
		"CUSTOM",
		"TOOL_CALL_RESULT", "CUSTOM",
	}
	if got := golden.FrameTypes(normalizedFrames(t, sink)); !reflect.DeepEqual(got, want) {
		t.Fatalf("types = %v, want %v", got, want)
	}
}

func TestCommittedMultiEventBlockReportsEveryTransportFailureBoundary(t *testing.T) {
	t.Parallel()
	id := agenticProjection(t).Public.Identity
	projection, err := convert.ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.Reasoning{Text: "reason"})}},
		convert.AgenticProjectionContext{Identity: id, Blocks: []convert.AgenticBlockContext{{BlockID: "reason"}}, Limits: convert.DefaultProjectionLimits()},
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt := convert.CommitReceiptV1{Revision: "revision", Domain: "projection", Identity: id, Digest: projection.Digest}
	// A committed reasoning block contains five native frames followed by its
	// authoritative custom supplement. Fail immediately before each frame.
	for allowed := 0; allowed < 6; allowed++ {
		t.Run(fmt.Sprintf("after_%d_frames", allowed), func(t *testing.T) {
			transport := &failAfterEventWrites{allowed: allowed}
			writer := bufio.NewWriter(transport)
			emit := NewObserverEmitter(t.Context(), writer, sse.NewSSEWriter())
			if emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
				t.Fatal("transport failure reported success")
			}
			if emit.Err() == nil || transport.writes != allowed {
				t.Fatalf("transport error=%v writes=%d, want %d", emit.Err(), transport.writes, allowed)
			}
			if frames := bytes.Count(transport.buffer.Bytes(), []byte("\n\n")); frames != allowed {
				t.Fatalf("complete prefix frames=%d, want %d\n%s", frames, allowed, transport.buffer.String())
			}
			if len(emit.agenticReceipts) != 0 {
				t.Fatal("partial transport prefix consumed the commit receipt")
			}
			if len(emit.agenticAttempts) != 0 {
				t.Fatal("partial transport prefix advanced the authoritative attempt")
			}
		})
	}
}

func TestCommittedLifecycleTransportFailureDoesNotAdvanceState(t *testing.T) {
	t.Parallel()
	id := agenticProjection(t).Public.Identity
	receiptFor := func(revision string, kind convert.AgenticEnvelopeKind, build func(*convert.AgenticEnvelopeV1)) convert.CommitReceiptV1 {
		envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: kind, Identity: id}
		build(envelope)
		digest, err := convert.LifecycleDigestV1(envelope)
		if err != nil {
			t.Fatal(err)
		}
		return convert.CommitReceiptV1{Revision: revision, Domain: "lifecycle", Kind: kind, Identity: id, Digest: digest}
	}

	t.Run("native prefix does not consume receipt", func(t *testing.T) {
		transport := &failAfterEventWrites{allowed: 1}
		emit := NewObserverEmitter(t.Context(), bufio.NewWriter(transport), sse.NewSSEWriter())
		fact := convert.LifecycleFactV1{}
		receipt := receiptFor("run-start", convert.EnvelopeRunStarted, func(envelope *convert.AgenticEnvelopeV1) { envelope.Lifecycle = &fact })
		if emit.RunStartedCommitted(fact, receipt) {
			t.Fatal("partial lifecycle transport reported success")
		}
		if transport.writes != 1 || len(emit.agenticReceipts) != 0 {
			t.Fatalf("writes=%d receipts=%d, want one native prefix and no consumed receipt", transport.writes, len(emit.agenticReceipts))
		}
	})

	t.Run("replacement failure does not authorize successor", func(t *testing.T) {
		projection := agenticProjection(t)
		transport := &failAfterEventWrites{allowed: len(projection.Blocks)}
		emit := NewObserverEmitter(t.Context(), bufio.NewWriter(transport), sse.NewSSEWriter())
		projectionReceipt := convert.CommitReceiptV1{Revision: "projection", Domain: "projection", Identity: id, Digest: projection.Digest}
		if !emit.EmitCommittedProjection(projection, projectionReceipt, DeliveryModeLiveContinuation) {
			t.Fatalf("projection setup failed: %v / %v", emit.Err(), emit.EncErr())
		}
		replacement := convert.AttemptReplacedV1{OldAttemptID: id.AttemptID, NewAttemptID: "attempt-new", Cause: "retry", Semantics: "replace"}
		receipt := receiptFor("replace", convert.EnvelopeAttemptReplaced, func(envelope *convert.AgenticEnvelopeV1) { envelope.AttemptReplaced = &replacement })
		if emit.AttemptReplaced(replacement, receipt) {
			t.Fatal("failed replacement transport reported success")
		}
		if got := emit.agenticAttempt(agenticMessageKey(id)); got != id.AttemptID {
			t.Fatalf("authoritative attempt=%q, want prior attempt %q", got, id.AttemptID)
		}
		if len(emit.agenticReceipts) != 1 {
			t.Fatalf("receipts=%d, want only the prior projection receipt", len(emit.agenticReceipts))
		}
		if _, consumed := emit.agenticReceipts[agenticReceiptKey(receipt)]; consumed {
			t.Fatal("failed replacement consumed its receipt")
		}
	})

	t.Run("pause and resume failures preserve prior state", func(t *testing.T) {
		targets := []convert.InterruptTargetV1{{ID: "one", Address: "agent:root;node:one"}, {ID: "two", Address: "agent:root;node:two"}}
		pause := convert.PausedV1{PauseID: "pause", Targets: targets}
		pauseReceipt := receiptFor("pause", convert.EnvelopePaused, func(envelope *convert.AgenticEnvelopeV1) { envelope.Paused = &pause })

		pauseFailure := &failAfterEventWrites{}
		failedPause := NewObserverEmitter(t.Context(), bufio.NewWriter(pauseFailure), sse.NewSSEWriter())
		if failedPause.Paused(pause, pauseReceipt) {
			t.Fatal("failed pause transport reported success")
		}
		if len(failedPause.agenticPauses) != 0 || len(failedPause.agenticReceipts) != 0 {
			t.Fatalf("pauses=%d receipts=%d, want no state advance", len(failedPause.agenticPauses), len(failedPause.agenticReceipts))
		}

		resumeFailure := &failAfterEventWrites{allowed: 1}
		emit := NewObserverEmitter(t.Context(), bufio.NewWriter(resumeFailure), sse.NewSSEWriter())
		if !emit.Paused(pause, pauseReceipt) {
			t.Fatalf("pause setup failed: %v / %v", emit.Err(), emit.EncErr())
		}
		resume := convert.ResumedV1{PauseID: "pause", Targets: targets[:1], NewTurnID: "turn-2", NewAttemptID: "attempt-2"}
		resumeReceipt := receiptFor("resume", convert.EnvelopeResumed, func(envelope *convert.AgenticEnvelopeV1) { envelope.Resumed = &resume })
		if emit.Resumed(resume, resumeReceipt) {
			t.Fatal("failed resume transport reported success")
		}
		stored, ok := emit.agenticPauses[agenticPauseKey(id, pause.PauseID)]
		if !ok || !reflect.DeepEqual(stored.targets, targets) {
			t.Fatalf("failed resume changed pause state: %#v", stored)
		}
		if _, consumed := emit.agenticReceipts[agenticReceiptKey(resumeReceipt)]; consumed {
			t.Fatal("failed resume consumed its receipt")
		}
	})
}

func TestCommittedProjectionEmitsResponseMetadata(t *testing.T) {
	t.Parallel()
	id := agenticProjection(t).Public.Identity
	projection, err := convert.ToAgenticProjection(
		&schema.AgenticMessage{
			Role:          schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "answer"})},
			ResponseMeta:  &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}},
		},
		convert.AgenticProjectionContext{Identity: id, Blocks: []convert.AgenticBlockContext{{BlockID: "text"}}, Limits: convert.DefaultProjectionLimits()},
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt := convert.CommitReceiptV1{Revision: "revision", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	if !emit.EmitCommittedProjection(projection, receipt, DeliveryModeLiveContinuation) {
		t.Fatalf("emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
	frames := normalizedFrames(t, sink)
	if got := golden.FrameTypes(frames); !reflect.DeepEqual(got, []string{"CUSTOM", "CUSTOM"}) {
		t.Fatalf("types = %v", got)
	}
	envelope, err := convert.DecodeAgenticEnvelope(frames[1].Data["value"])
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Kind != convert.EnvelopeResponseMeta || envelope.ResponseMeta == nil || envelope.ResponseMeta.TokenUsage == nil || *envelope.ResponseMeta.TokenUsage.TotalTokens != 5 {
		t.Fatalf("response metadata envelope = %#v", envelope)
	}
}

func TestAgenticStreamMatchesNormalizedGoldenFixture(t *testing.T) {
	t.Parallel()
	id := convert.AgenticIdentityV1{SessionID: "session-golden", ThreadID: "session-golden", RunID: "run-golden", TurnID: "turn-golden", MessageID: "message-golden", AttemptID: "attempt-golden", AgentPath: []convert.AgentPathSegment{{Name: "root", RunID: "run-golden"}}}
	projection, err := convert.ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "hello"})}, ResponseMeta: &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}}},
		convert.AgenticProjectionContext{Identity: id, Blocks: []convert.AgenticBlockContext{{BlockID: "block-golden"}}, Limits: convert.DefaultProjectionLimits()},
	)
	if err != nil {
		t.Fatal(err)
	}
	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	transient, err := convert.TransientEventForBlock(projection.Blocks[0].Public)
	if err != nil || !emit.EmitTransientBlock(transient) {
		t.Fatalf("transient: %v / %v", err, emit.EncErr())
	}
	projectionReceipt := convert.CommitReceiptV1{Revision: "revision-golden", Domain: "projection", Identity: id, Digest: projection.Digest}
	if !emit.EmitCommittedProjection(projection, projectionReceipt, DeliveryModeLiveContinuation) {
		t.Fatalf("projection: %v / %v", emit.Err(), emit.EncErr())
	}
	targets := []convert.InterruptTargetV1{{ID: "interrupt-golden", Address: "agent:root;node:approval"}}
	pause := convert.PausedV1{PauseID: "pause-golden", Targets: targets}
	pauseEnvelope := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopePaused, Identity: id, Paused: &pause}
	pauseDigest, err := convert.LifecycleDigestV1(pauseEnvelope)
	if err != nil || !emit.Paused(pause, convert.CommitReceiptV1{Revision: "pause-revision", Domain: "lifecycle", Kind: convert.EnvelopePaused, Identity: id, Digest: pauseDigest}) {
		t.Fatalf("pause: %v / %v", err, emit.EncErr())
	}
	resume := convert.ResumedV1{PauseID: "pause-golden", Targets: targets, Full: true, NewTurnID: "turn-resumed", NewAttemptID: "attempt-resumed"}
	resumeEnvelope := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeResumed, Identity: id, Resumed: &resume}
	resumeDigest, err := convert.LifecycleDigestV1(resumeEnvelope)
	if err != nil || !emit.Resumed(resume, convert.CommitReceiptV1{Revision: "resume-revision", Domain: "lifecycle", Kind: convert.EnvelopeResumed, Identity: id, Digest: resumeDigest}) {
		t.Fatalf("resume: %v / %v", err, emit.EncErr())
	}
	got := normalizedFrames(t, sink)
	content, err := os.ReadFile(filepath.Join("..", "testdata", "golden", "agentic_stream.normalized.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Frames []golden.Frame `json:"frames"`
	}
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, fixture.Frames) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(fixture.Frames, "", "  ")
		t.Fatalf("agentic stream golden mismatch\ngot: %s\nwant: %s", gotJSON, wantJSON)
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
	complete := agenticProjection(t)
	toolResult, err := convert.ToAgenticProjection(
		&schema.AgenticMessage{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.FunctionToolResult{
			CallID: "call", Name: "lookup", Content: []*schema.FunctionToolResultContentBlock{{Type: schema.FunctionToolResultContentBlockTypeText, Text: &schema.UserInputText{Text: "result"}}},
		})}},
		convert.AgenticProjectionContext{Identity: complete.Public.Identity, Blocks: []convert.AgenticBlockContext{{BlockID: "result"}}, Limits: convert.DefaultProjectionLimits()},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		projection *convert.AgenticProjection
	}{{"complete message", complete}, {"tool result", toolResult}} {
		t.Run(tc.name, func(t *testing.T) {
			sink := testsse.NewSink()
			emit := NewObserverEmitter(context.Background(), sink.Writer(), sink.SSEWriter())
			receipt := convert.CommitReceiptV1{Revision: "rev-1", Domain: "projection", Identity: tc.projection.Public.Identity, Digest: "wrong"}
			if emit.EmitCommittedProjection(tc.projection, receipt, DeliveryModeReplay) {
				t.Fatal("mismatched receipt emitted")
			}
			if got := normalizedFrames(t, sink); len(got) != 0 {
				t.Fatalf("frames = %#v, want none", got)
			}
		})
	}
}

func TestCommittedReceiptIsConsumedOncePerEmitter(t *testing.T) {
	t.Parallel()
	projection := agenticProjection(t)
	receipt := convert.CommitReceiptV1{Revision: "revision", Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}
	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	if !emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
		t.Fatalf("first emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
	first := len(normalizedFrames(t, sink))
	if emit.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
		t.Fatal("replayed receipt emitted twice")
	}
	if got := len(normalizedFrames(t, sink)); got != first {
		t.Fatalf("receipt replay wrote %d frames, want %d", got, first)
	}

	freshSink := testsse.NewSink()
	fresh := NewObserverEmitter(t.Context(), freshSink.Writer(), freshSink.SSEWriter())
	if !fresh.EmitCommittedProjection(projection, receipt, DeliveryModeReplay) {
		t.Fatalf("fresh replay failed: %v / %v", fresh.Err(), fresh.EncErr())
	}
}

func TestAgenticStateKeysCannotCollideAtEmbeddedDelimiters(t *testing.T) {
	t.Parallel()
	left := agenticProjection(t).Public.Identity
	right := left
	left.SessionID = "a\x00b"
	left.ThreadID = left.SessionID
	left.RunID = "c"
	right.SessionID = "a"
	right.ThreadID = right.SessionID
	right.RunID = "b\x00c"
	if agenticMessageKey(left) == agenticMessageKey(right) {
		t.Fatal("distinct message identities produced the same state key")
	}
	if agenticPauseKey(left, "pause") == agenticPauseKey(right, "pause") {
		t.Fatal("distinct pause identities produced the same state key")
	}
	leftReceipt := convert.CommitReceiptV1{Revision: "rev\x00projection", Domain: "domain", Identity: left, Digest: "digest"}
	rightReceipt := convert.CommitReceiptV1{Revision: "rev", Domain: "projection\x00domain", Identity: left, Digest: "digest"}
	if agenticReceiptKey(leftReceipt) == agenticReceiptKey(rightReceipt) {
		t.Fatal("distinct receipts produced the same state key")
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
			return e.Paused(convert.PausedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}}, convert.CommitReceiptV1{Revision: "failed", Domain: "lifecycle", Kind: convert.EnvelopePaused, Identity: root, Digest: "not-committed"})
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

func TestTwoCommittedTurnsRequireOneExplicitRunTerminal(t *testing.T) {
	t.Parallel()
	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	turnOne := agenticProjection(t).Public.Identity
	turnTwo := turnOne
	turnTwo.TurnID = "turn-2"
	turnTwo.MessageID = "message-2"
	turnTwo.AttemptID = "attempt-2"
	emitFact := func(revision string, kind convert.AgenticEnvelopeKind, identity convert.AgenticIdentityV1, fact convert.LifecycleFactV1) bool {
		envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: kind, Identity: identity, Lifecycle: &fact}
		digest, err := convert.LifecycleDigestV1(envelope)
		if err != nil {
			t.Fatal(err)
		}
		receipt := convert.CommitReceiptV1{Revision: revision, Domain: "lifecycle", Kind: kind, Identity: identity, Digest: digest}
		switch kind {
		case convert.EnvelopeRunStarted:
			return emit.RunStartedCommitted(fact, receipt)
		case convert.EnvelopeTurnStarted:
			return emit.TurnStarted(fact, receipt)
		case convert.EnvelopeTurnFinished:
			return emit.TurnFinished(fact, receipt)
		case convert.EnvelopeRunFinished:
			return emit.RunFinishedCommitted(fact, receipt)
		default:
			t.Fatalf("unsupported lifecycle kind %q", kind)
			return false
		}
	}
	for _, step := range []struct {
		revision string
		kind     convert.AgenticEnvelopeKind
		identity convert.AgenticIdentityV1
		fact     convert.LifecycleFactV1
	}{
		{"run-start", convert.EnvelopeRunStarted, turnOne, convert.LifecycleFactV1{}},
		{"turn-1-start", convert.EnvelopeTurnStarted, turnOne, convert.LifecycleFactV1{}},
		{"turn-1-finish", convert.EnvelopeTurnFinished, turnOne, convert.LifecycleFactV1{Detail: "done"}},
	} {
		if !emitFact(step.revision, step.kind, step.identity, step.fact) {
			t.Fatalf("%s failed: %v / %v", step.revision, emit.Err(), emit.EncErr())
		}
	}
	if got := golden.FrameTypes(normalizedFrames(t, sink)); !reflect.DeepEqual(got, []string{"RUN_STARTED", "CUSTOM", "CUSTOM", "CUSTOM"}) {
		t.Fatalf("first turn types = %v", got)
	}
	for _, step := range []struct {
		revision string
		kind     convert.AgenticEnvelopeKind
		fact     convert.LifecycleFactV1
	}{
		{"turn-2-start", convert.EnvelopeTurnStarted, convert.LifecycleFactV1{}},
		{"turn-2-finish", convert.EnvelopeTurnFinished, convert.LifecycleFactV1{Detail: "done"}},
		{"run-finish", convert.EnvelopeRunFinished, convert.LifecycleFactV1{LoopSettled: true}},
	} {
		if !emitFact(step.revision, step.kind, turnTwo, step.fact) {
			t.Fatalf("%s failed: %v / %v", step.revision, emit.Err(), emit.EncErr())
		}
	}
	want := []string{"RUN_STARTED", "CUSTOM", "CUSTOM", "CUSTOM", "CUSTOM", "CUSTOM", "RUN_FINISHED", "CUSTOM"}
	if got := golden.FrameTypes(normalizedFrames(t, sink)); !reflect.DeepEqual(got, want) {
		t.Fatalf("two-turn types = %v, want %v", got, want)
	}
}

func TestCommittedPauseResumeOrderingAndTargets(t *testing.T) {
	t.Parallel()
	id := agenticProjection(t).Public.Identity
	targets := []convert.InterruptTargetV1{
		{ID: "one", Address: "agent:root;node:one"},
		{ID: "two", Address: "agent:root;node:two"},
		{ID: "three", Address: "agent:root;node:three"},
	}
	lifecycleReceipt := func(kind convert.AgenticEnvelopeKind, build func(*convert.AgenticEnvelopeV1)) convert.CommitReceiptV1 {
		envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: kind, Identity: id}
		build(envelope)
		digest, err := convert.LifecycleDigestV1(envelope)
		if err != nil {
			t.Fatal(err)
		}
		return convert.CommitReceiptV1{Revision: "revision", Domain: "lifecycle", Kind: kind, Identity: id, Digest: digest}
	}
	emitResume := func(e *Emitter, resume convert.ResumedV1) bool {
		receipt := lifecycleReceipt(convert.EnvelopeResumed, func(envelope *convert.AgenticEnvelopeV1) { envelope.Resumed = &resume })
		return e.Resumed(resume, receipt)
	}

	missingSink := testsse.NewSink()
	missing := NewObserverEmitter(t.Context(), missingSink.Writer(), missingSink.SSEWriter())
	if emitResume(missing, convert.ResumedV1{PauseID: "pause", Targets: targets, Full: true, NewTurnID: "turn-2", NewAttemptID: "attempt-2"}) {
		t.Fatal("resume emitted before pause")
	}
	if got := normalizedFrames(t, missingSink); len(got) != 0 {
		t.Fatalf("frames before pause = %#v", got)
	}

	sink := testsse.NewSink()
	emit := NewObserverEmitter(t.Context(), sink.Writer(), sink.SSEWriter())
	correlation := &convert.ApprovalInterruptCorrelation{ApprovalRequestID: "approval", InterruptTargetID: targets[2].ID, InterruptAddress: targets[2].Address}
	pause := convert.PausedV1{PauseID: "pause", Targets: targets, Correlation: correlation}
	pauseReceipt := lifecycleReceipt(convert.EnvelopePaused, func(envelope *convert.AgenticEnvelopeV1) { envelope.Paused = &pause })
	if !emit.Paused(pause, pauseReceipt) {
		t.Fatalf("pause emit failed: %v / %v", emit.Err(), emit.EncErr())
	}
	duplicateReceipt := pauseReceipt
	duplicateReceipt.Revision = "different-revision"
	if emit.Paused(pause, duplicateReceipt) {
		t.Fatal("duplicate pause ID was committed twice")
	}
	if emitResume(emit, convert.ResumedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{targets[2], targets[0]}, NewTurnID: "turn-2", NewAttemptID: "attempt-2"}) {
		t.Fatal("out-of-order subset resumed")
	}
	if emitResume(emit, convert.ResumedV1{PauseID: "pause", Targets: targets[:2], Full: true, NewTurnID: "turn-2", NewAttemptID: "attempt-2"}) {
		t.Fatal("incomplete full target set resumed")
	}
	if !emitResume(emit, convert.ResumedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{targets[1]}, NewTurnID: "turn-2", NewAttemptID: "attempt-2"}) {
		t.Fatalf("partial resume failed: %v / %v", emit.Err(), emit.EncErr())
	}
	remaining := []convert.InterruptTargetV1{targets[0], targets[2]}
	wrongCorrelation := *correlation
	wrongCorrelation.ApprovalRequestID = "different"
	if emitResume(emit, convert.ResumedV1{PauseID: "pause", Targets: remaining, Full: true, NewTurnID: "turn-3", NewAttemptID: "attempt-3", Correlation: &wrongCorrelation}) {
		t.Fatal("mismatched resume correlation was accepted")
	}
	if !emitResume(emit, convert.ResumedV1{PauseID: "pause", Targets: remaining, Full: true, NewTurnID: "turn-3", NewAttemptID: "attempt-3", Correlation: correlation}) {
		t.Fatalf("full resume failed: %v / %v", emit.Err(), emit.EncErr())
	}
	if got := golden.FrameTypes(normalizedFrames(t, sink)); !reflect.DeepEqual(got, []string{"CUSTOM", "CUSTOM", "CUSTOM"}) {
		t.Fatalf("types = %v", got)
	}
	freshSink := testsse.NewSink()
	fresh := NewObserverEmitter(t.Context(), freshSink.Writer(), freshSink.SSEWriter())
	if !fresh.Paused(pause, pauseReceipt) ||
		!emitResume(fresh, convert.ResumedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{targets[1]}, NewTurnID: "turn-2", NewAttemptID: "attempt-2"}) ||
		!emitResume(fresh, convert.ResumedV1{PauseID: "pause", Targets: remaining, Full: true, NewTurnID: "turn-3", NewAttemptID: "attempt-3", Correlation: correlation}) {
		t.Fatalf("fresh pause/resume replay failed: %v / %v", fresh.Err(), fresh.EncErr())
	}
	if got, want := normalizedFrames(t, freshSink), normalizedFrames(t, sink); !reflect.DeepEqual(got, want) {
		t.Fatalf("fresh pause/resume replay mismatch\ngot=%#v\nwant=%#v", got, want)
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
			v.AttemptReplaced = &convert.AttemptReplacedV1{OldAttemptID: root.AttemptID, NewAttemptID: "new", Cause: "retry", Semantics: "replace"}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.AttemptReplaced(convert.AttemptReplacedV1{OldAttemptID: root.AttemptID, NewAttemptID: "new", Cause: "retry", Semantics: "replace"}, r)
		}, []string{"CUSTOM"}},
		{"paused", convert.EnvelopePaused, root, func(v *convert.AgenticEnvelopeV1) {
			v.Paused = &convert.PausedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.Paused(convert.PausedV1{PauseID: "pause", Targets: []convert.InterruptTargetV1{{ID: "interrupt", Address: "agent:root"}}}, r)
		}, []string{"CUSTOM"}},
		{"cancelled", convert.EnvelopeCancelled, root, func(v *convert.AgenticEnvelopeV1) {
			v.Cancelled = &convert.CancelledV1{RequestedMode: convert.CancellationModeAfterChatModel, ObservedMode: convert.CancellationModeImmediate, Classification: convert.CancellationClassTimeout}
		}, func(e *Emitter, r convert.CommitReceiptV1) bool {
			return e.Cancelled(convert.CancelledV1{RequestedMode: convert.CancellationModeAfterChatModel, ObservedMode: convert.CancellationModeImmediate, Classification: convert.CancellationClassTimeout}, r)
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
