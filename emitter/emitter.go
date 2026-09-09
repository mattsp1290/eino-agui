package emitter

import (
	"bufio"
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
)

// Emitter serializes AG-UI events to an SSE stream. It records the first
// transport error and becomes a no-op afterward, so stream code can check Err
// at convenient points.
//
// When the first transport error is detected, cancel is invoked if it is set.
// This is the client-disconnect signal used by fasthttp-backed callers: the
// request context does not report disconnects, but failed SSE writes do.
type Emitter struct {
	ctx                    context.Context
	w                      *bufio.Writer
	sse                    *sse.SSEWriter
	threadID               string
	runID                  string
	cancel                 context.CancelFunc
	err                    error
	encErr                 error
	openTextID             string
	openReasoningID        string
	openReasoningMessageID string
	startedToolCalls       map[string]struct{}
	endedToolCalls         map[string]struct{}
}

// NewEmitter builds an Emitter bound to a request's concrete SSE writer pair.
// cancel may be nil; when non-nil it is called once, on the first transport
// write or flush error.
func NewEmitter(ctx context.Context, w *bufio.Writer, sw *sse.SSEWriter, threadID, runID string, cancel context.CancelFunc) *Emitter {
	return &Emitter{ctx: ctx, w: w, sse: sw, threadID: threadID, runID: runID, cancel: cancel}
}

// Err returns the first transport error, if any.
func (e *Emitter) Err() error { return e.err }

// EncErr returns the first event encoding or validation error, if any. Encoding
// errors drop the malformed event but do not stop later writes.
func (e *Emitter) EncErr() error { return e.encErr }

// Emit sends a caller-built event through the same validation, encoding, and
// transport error path used by all typed helpers. Callers own protocol ordering.
func (e *Emitter) Emit(ev events.Event) bool {
	if e.err != nil {
		return false
	}
	if ev == nil || isNilEvent(ev) {
		if e.encErr == nil {
			e.encErr = fmt.Errorf("cannot emit a nil AG-UI event")
		}
		return false
	}
	if err := e.sse.WriteEvent(e.ctx, e.w, ev); err != nil {
		if isTransportError(err) {
			e.err = err
			if e.cancel != nil {
				e.cancel()
			}
			return false
		}
		if e.encErr == nil {
			e.encErr = err
		}
		return false
	}
	return true
}

func isNilEvent(ev events.Event) bool {
	value := reflect.ValueOf(ev)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func isTransportError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "SSE write failed:") ||
		strings.HasPrefix(msg, "SSE flush failed:")
}

// RunStarted emits RUN_STARTED.
func (e *Emitter) RunStarted() { e.Emit(events.NewRunStartedEvent(e.threadID, e.runID)) }

// RunFinishedSuccess emits a successful RUN_FINISHED.
func (e *Emitter) RunFinishedSuccess(usage ...events.TokenUsage) {
	options := []events.RunFinishedOption{events.WithSuccessOutcome()}
	if len(usage) > 0 {
		options = append(options, events.WithUsage(usage))
	}
	e.Emit(events.NewRunFinishedEventWithOptions(e.threadID, e.runID, options...))
}

// RunFinishedInterrupt emits RUN_FINISHED with interrupt outcome metadata.
func (e *Emitter) RunFinishedInterrupt(interrupts []types.Interrupt, usage ...events.TokenUsage) {
	options := []events.RunFinishedOption{events.WithInterruptOutcome(interrupts)}
	if len(usage) > 0 {
		options = append(options, events.WithUsage(usage))
	}
	e.Emit(events.NewRunFinishedEventWithOptions(e.threadID, e.runID, options...))
}

// RunError emits RUN_ERROR for this emitter's run ID.
func (e *Emitter) RunError(msg string, usage ...events.TokenUsage) {
	options := []events.RunErrorOption{events.WithRunID(e.runID)}
	if len(usage) > 0 {
		options = append(options, events.WithErrorUsage(usage))
	}
	e.Emit(events.NewRunErrorEvent(msg, options...))
}

// StepStarted emits STEP_STARTED.
func (e *Emitter) StepStarted(name string) { e.Emit(events.NewStepStartedEvent(name)) }

// StepFinished emits STEP_FINISHED.
func (e *Emitter) StepFinished(name string) { e.Emit(events.NewStepFinishedEvent(name)) }

// SubagentStarted emits SUBAGENT_STARTED.
func (e *Emitter) SubagentStarted(subagentRunID, name string, options ...events.SubagentStartedOption) {
	e.Emit(events.NewSubagentStartedEvent(subagentRunID, name, options...))
}

// SubagentFinished emits SUBAGENT_FINISHED.
func (e *Emitter) SubagentFinished(subagentRunID string, options ...events.SubagentFinishedOption) {
	e.Emit(events.NewSubagentFinishedEvent(subagentRunID, options...))
}

// SubagentError emits SUBAGENT_ERROR.
func (e *Emitter) SubagentError(subagentRunID, message string, options ...events.SubagentErrorOption) {
	e.Emit(events.NewSubagentErrorEvent(subagentRunID, message, options...))
}

// TextStart emits TEXT_MESSAGE_START with assistant role.
func (e *Emitter) TextStart(id string) {
	wrote := e.Emit(events.NewTextMessageStartEvent(id, events.WithRole("assistant")))
	if id != "" && wrote {
		e.openTextID = id
	}
}

// TextContent emits TEXT_MESSAGE_CONTENT unless delta is empty.
func (e *Emitter) TextContent(id, delta string) {
	if delta == "" {
		return
	}
	e.Emit(events.NewTextMessageContentEvent(id, delta))
}

// TextEnd emits TEXT_MESSAGE_END.
func (e *Emitter) TextEnd(id string) {
	e.Emit(events.NewTextMessageEndEvent(id))
	if e.openTextID == id {
		e.openTextID = ""
	}
}

// ReasoningStart emits REASONING_START.
func (e *Emitter) ReasoningStart(id string) {
	if e.Emit(events.NewReasoningStartEvent(id)) && id != "" {
		e.openReasoningID = id
	}
}

// ReasoningMessageStart emits REASONING_MESSAGE_START with reasoning role.
func (e *Emitter) ReasoningMessageStart(id string) {
	wrote := e.Emit(events.NewReasoningMessageStartEvent(id, "reasoning"))
	if id != "" && wrote {
		e.openReasoningMessageID = id
	}
}

// ReasoningContent emits REASONING_MESSAGE_CONTENT unless delta is empty.
func (e *Emitter) ReasoningContent(id, delta string) {
	if delta == "" {
		return
	}
	e.Emit(events.NewReasoningMessageContentEvent(id, delta))
}

// ReasoningMessageEnd emits REASONING_MESSAGE_END.
func (e *Emitter) ReasoningMessageEnd(id string) {
	e.Emit(events.NewReasoningMessageEndEvent(id))
	if e.openReasoningMessageID == id {
		e.openReasoningMessageID = ""
	}
}

// ReasoningEnd emits REASONING_END.
func (e *Emitter) ReasoningEnd(id string) {
	e.Emit(events.NewReasoningEndEvent(id))
	if e.openReasoningID == id {
		e.openReasoningID = ""
	}
}

// ToolStart emits TOOL_CALL_START.
func (e *Emitter) ToolStart(toolCallID, name, parentMessageID string) {
	if toolCallID == "" || name == "" {
		return
	}
	e.closeOpenBlocks()
	if e.toolStarted(toolCallID) || e.toolEnded(toolCallID) {
		return
	}
	options := []events.ToolCallStartOption{}
	if parentMessageID != "" {
		options = append(options, events.WithParentMessageID(parentMessageID))
	}
	if e.Emit(events.NewToolCallStartEvent(toolCallID, name, options...)) {
		e.markToolStarted(toolCallID)
	}
}

// ToolArgs emits TOOL_CALL_ARGS unless delta is empty.
func (e *Emitter) ToolArgs(toolCallID, delta string) {
	if toolCallID == "" || delta == "" || !e.toolStarted(toolCallID) || e.toolEnded(toolCallID) {
		return
	}
	e.closeOpenBlocks()
	e.Emit(events.NewToolCallArgsEvent(toolCallID, delta))
}

// ToolEnd emits TOOL_CALL_END.
func (e *Emitter) ToolEnd(toolCallID string) {
	if toolCallID == "" || !e.toolStarted(toolCallID) || e.toolEnded(toolCallID) {
		return
	}
	e.closeOpenBlocks()
	if e.Emit(events.NewToolCallEndEvent(toolCallID)) {
		e.markToolEnded(toolCallID)
	}
}

// ToolResult emits TOOL_CALL_RESULT. Empty content is normalized to "(empty)"
// because the AG-UI SDK rejects empty result payloads.
func (e *Emitter) ToolResult(messageID, toolCallID, content string) {
	if toolCallID == "" {
		return
	}
	if content == "" {
		content = "(empty)"
	}
	e.closeOpenBlocks()
	e.Emit(events.NewToolCallResultEvent(messageID, toolCallID, content))
}

func (e *Emitter) closeOpenBlocks() {
	if e.openTextID != "" {
		id := e.openTextID
		e.openTextID = ""
		e.Emit(events.NewTextMessageEndEvent(id))
	}
	if e.openReasoningMessageID != "" {
		id := e.openReasoningMessageID
		e.openReasoningMessageID = ""
		e.Emit(events.NewReasoningMessageEndEvent(id))
	}
	if e.openReasoningID != "" {
		id := e.openReasoningID
		e.openReasoningID = ""
		e.Emit(events.NewReasoningEndEvent(id))
	}
}

func (e *Emitter) toolStarted(toolCallID string) bool {
	if e.startedToolCalls == nil {
		return false
	}
	_, ok := e.startedToolCalls[toolCallID]
	return ok
}

func (e *Emitter) markToolStarted(toolCallID string) {
	if e.startedToolCalls == nil {
		e.startedToolCalls = make(map[string]struct{})
	}
	e.startedToolCalls[toolCallID] = struct{}{}
}

func (e *Emitter) toolEnded(toolCallID string) bool {
	if e.endedToolCalls == nil {
		return false
	}
	_, ok := e.endedToolCalls[toolCallID]
	return ok
}

func (e *Emitter) markToolEnded(toolCallID string) {
	if e.endedToolCalls == nil {
		e.endedToolCalls = make(map[string]struct{})
	}
	e.endedToolCalls[toolCallID] = struct{}{}
}

// StateSnapshot emits STATE_SNAPSHOT.
func (e *Emitter) StateSnapshot(snapshot any) {
	e.Emit(events.NewStateSnapshotEvent(snapshot))
}

// StateDelta emits STATE_DELTA unless ops is empty.
func (e *Emitter) StateDelta(ops []events.JSONPatchOperation) {
	if len(ops) == 0 {
		return
	}
	e.Emit(events.NewStateDeltaEvent(ops))
}

// MessagesSnapshot emits MESSAGES_SNAPSHOT after removing encrypted reasoning
// blobs from the client-facing copy.
func (e *Emitter) MessagesSnapshot(msgs []types.Message) {
	e.Emit(events.NewMessagesSnapshotEvent(scrubEncryptedValues(msgs)))
}

func scrubEncryptedValues(msgs []types.Message) []types.Message {
	needsScrub := false
	for i := range msgs {
		if msgs[i].EncryptedValue != "" || msgs[i].EncryptedContent != "" || hasEncryptedToolCall(msgs[i].ToolCalls) {
			needsScrub = true
			break
		}
	}
	if !needsScrub {
		return msgs
	}
	out := make([]types.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].EncryptedValue = ""
		out[i].EncryptedContent = ""
		if len(msgs[i].ToolCalls) > 0 {
			out[i].ToolCalls = append([]types.ToolCall(nil), msgs[i].ToolCalls...)
			for j := range out[i].ToolCalls {
				out[i].ToolCalls[j].EncryptedValue = nil
			}
		}
	}
	return out
}

func hasEncryptedToolCall(calls []types.ToolCall) bool {
	for i := range calls {
		if calls[i].EncryptedValue != nil {
			return true
		}
	}
	return false
}

// ActivitySnapshot emits ACTIVITY_SNAPSHOT.
func (e *Emitter) ActivitySnapshot(messageID, activityType string, content any) {
	e.Emit(events.NewActivitySnapshotEvent(messageID, activityType, content))
}

// ActivityDelta emits ACTIVITY_DELTA unless patch is empty.
func (e *Emitter) ActivityDelta(messageID, activityType string, patch []events.JSONPatchOperation) {
	if len(patch) == 0 {
		return
	}
	e.Emit(events.NewActivityDeltaEvent(messageID, activityType, patch))
}

// ReasoningEncryptedValue emits REASONING_ENCRYPTED_VALUE.
func (e *Emitter) ReasoningEncryptedValue(subtype events.ReasoningEncryptedValueSubtype, entityID, encryptedValue string) {
	e.Emit(events.NewReasoningEncryptedValueEvent(subtype, entityID, encryptedValue))
}

// Custom emits CUSTOM with the given name and value.
func (e *Emitter) Custom(name string, value any) {
	e.Emit(events.NewCustomEvent(name, events.WithValue(value)))
}
