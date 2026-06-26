package emitter

import (
	"bufio"
	"context"
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

func (e *Emitter) write(ev events.Event) {
	if e.err != nil {
		return
	}
	if err := e.sse.WriteEvent(e.ctx, e.w, ev); err != nil {
		if isTransportError(err) {
			e.err = err
			if e.cancel != nil {
				e.cancel()
			}
			return
		}
		if e.encErr == nil {
			e.encErr = err
		}
	}
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
func (e *Emitter) RunStarted() { e.write(events.NewRunStartedEvent(e.threadID, e.runID)) }

// RunFinishedSuccess emits a successful RUN_FINISHED.
func (e *Emitter) RunFinishedSuccess() {
	e.write(events.NewRunFinishedEventWithOptions(e.threadID, e.runID, events.WithSuccessOutcome()))
}

// RunFinishedInterrupt emits RUN_FINISHED with interrupt outcome metadata.
func (e *Emitter) RunFinishedInterrupt(interrupts []types.Interrupt) {
	e.write(events.NewRunFinishedEventWithOptions(e.threadID, e.runID, events.WithInterruptOutcome(interrupts)))
}

// RunError emits RUN_ERROR for this emitter's run ID.
func (e *Emitter) RunError(msg string) {
	e.write(events.NewRunErrorEvent(msg, events.WithRunID(e.runID)))
}

// StepStarted emits STEP_STARTED.
func (e *Emitter) StepStarted(name string) { e.write(events.NewStepStartedEvent(name)) }

// StepFinished emits STEP_FINISHED.
func (e *Emitter) StepFinished(name string) { e.write(events.NewStepFinishedEvent(name)) }

// TextStart emits TEXT_MESSAGE_START with assistant role.
func (e *Emitter) TextStart(id string) {
	encErr := e.encErr
	e.write(events.NewTextMessageStartEvent(id, events.WithRole("assistant")))
	if id != "" && e.err == nil && e.encErr == encErr {
		e.openTextID = id
	}
}

// TextContent emits TEXT_MESSAGE_CONTENT unless delta is empty.
func (e *Emitter) TextContent(id, delta string) {
	if delta == "" {
		return
	}
	e.write(events.NewTextMessageContentEvent(id, delta))
}

// TextEnd emits TEXT_MESSAGE_END.
func (e *Emitter) TextEnd(id string) {
	e.write(events.NewTextMessageEndEvent(id))
	if e.openTextID == id {
		e.openTextID = ""
	}
}

// ReasoningStart emits REASONING_START.
func (e *Emitter) ReasoningStart(id string) { e.write(events.NewReasoningStartEvent(id)) }

// ReasoningMessageStart emits REASONING_MESSAGE_START with assistant role.
func (e *Emitter) ReasoningMessageStart(id string) {
	encErr := e.encErr
	e.write(events.NewReasoningMessageStartEvent(id, "assistant"))
	if id != "" && e.err == nil && e.encErr == encErr {
		e.openReasoningMessageID = id
	}
}

// ReasoningContent emits REASONING_MESSAGE_CONTENT unless delta is empty.
func (e *Emitter) ReasoningContent(id, delta string) {
	if delta == "" {
		return
	}
	e.write(events.NewReasoningMessageContentEvent(id, delta))
}

// ReasoningMessageEnd emits REASONING_MESSAGE_END.
func (e *Emitter) ReasoningMessageEnd(id string) {
	e.write(events.NewReasoningMessageEndEvent(id))
	if e.openReasoningMessageID == id {
		e.openReasoningMessageID = ""
	}
}

// ReasoningEnd emits REASONING_END.
func (e *Emitter) ReasoningEnd(id string) { e.write(events.NewReasoningEndEvent(id)) }

// ToolStart emits TOOL_CALL_START.
func (e *Emitter) ToolStart(toolCallID, name string) {
	if toolCallID == "" || name == "" {
		return
	}
	e.closeOpenBlocks()
	if e.toolStarted(toolCallID) || e.toolEnded(toolCallID) {
		return
	}
	e.markToolStarted(toolCallID)
	e.write(events.NewToolCallStartEvent(toolCallID, name))
}

// ToolArgs emits TOOL_CALL_ARGS unless delta is empty.
func (e *Emitter) ToolArgs(toolCallID, delta string) {
	if toolCallID == "" || delta == "" || !e.toolStarted(toolCallID) || e.toolEnded(toolCallID) {
		return
	}
	e.closeOpenBlocks()
	e.write(events.NewToolCallArgsEvent(toolCallID, delta))
}

// ToolEnd emits TOOL_CALL_END.
func (e *Emitter) ToolEnd(toolCallID string) {
	if toolCallID == "" || !e.toolStarted(toolCallID) || e.toolEnded(toolCallID) {
		return
	}
	e.closeOpenBlocks()
	e.write(events.NewToolCallEndEvent(toolCallID))
	e.markToolEnded(toolCallID)
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
	e.write(events.NewToolCallResultEvent(messageID, toolCallID, content))
}

// NewToolCallBuffer returns a per-call buffer that delays TOOL_CALL_START until
// both a non-empty tool-call ID and function name are known. Stream code should
// own the stable key/map for multiple calls and use one buffer per streamed call.
func (e *Emitter) NewToolCallBuffer() *ToolCallBuffer {
	return &ToolCallBuffer{emit: e}
}

// ToolCallBuffer buffers streamed tool-call argument fragments until the call
// can be started with a valid ID and function name.
type ToolCallBuffer struct {
	emit        *Emitter
	id          string
	name        string
	pendingArgs []string
	started     bool
	ended       bool
}

// Update records any newly available ID/name and emits non-empty args once the
// call has started. Malformed partial updates are buffered or ignored rather
// than passed to the SDK.
func (b *ToolCallBuffer) Update(id, name, argsDelta string) {
	if b == nil || b.emit == nil || b.ended {
		return
	}
	if id != "" {
		b.id = id
	}
	if name != "" {
		b.name = name
	}
	if argsDelta != "" {
		b.pendingArgs = append(b.pendingArgs, argsDelta)
	}
	b.startIfReady()
}

// End emits TOOL_CALL_END if the buffered call had enough data to start.
func (b *ToolCallBuffer) End() {
	if b == nil || b.emit == nil || b.ended {
		return
	}
	b.startIfReady()
	if b.started {
		b.emit.ToolEnd(b.id)
	}
	b.ended = true
}

func (b *ToolCallBuffer) startIfReady() {
	if b.started || b.id == "" || b.name == "" {
		return
	}
	b.emit.ToolStart(b.id, b.name)
	b.started = true
	for _, arg := range b.pendingArgs {
		b.emit.ToolArgs(b.id, arg)
	}
	b.pendingArgs = nil
}

func (e *Emitter) closeOpenBlocks() {
	if e.openTextID != "" {
		id := e.openTextID
		e.openTextID = ""
		e.write(events.NewTextMessageEndEvent(id))
	}
	if e.openReasoningMessageID != "" {
		id := e.openReasoningMessageID
		e.openReasoningMessageID = ""
		e.write(events.NewReasoningMessageEndEvent(id))
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
	e.write(events.NewStateSnapshotEvent(snapshot))
}

// StateDelta emits STATE_DELTA unless ops is empty.
func (e *Emitter) StateDelta(ops []events.JSONPatchOperation) {
	if len(ops) == 0 {
		return
	}
	e.write(events.NewStateDeltaEvent(ops))
}

// MessagesSnapshot emits MESSAGES_SNAPSHOT after removing encrypted reasoning
// blobs from the client-facing copy.
func (e *Emitter) MessagesSnapshot(msgs []types.Message) {
	e.write(events.NewMessagesSnapshotEvent(scrubEncryptedValues(msgs)))
}

func scrubEncryptedValues(msgs []types.Message) []types.Message {
	needsScrub := false
	for i := range msgs {
		if msgs[i].EncryptedValue != "" || msgs[i].EncryptedContent != "" {
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
	}
	return out
}

// ActivitySnapshot emits ACTIVITY_SNAPSHOT.
func (e *Emitter) ActivitySnapshot(messageID, activityType string, content any) {
	e.write(events.NewActivitySnapshotEvent(messageID, activityType, content))
}

// ActivityDelta emits ACTIVITY_DELTA unless patch is empty.
func (e *Emitter) ActivityDelta(messageID, activityType string, patch []events.JSONPatchOperation) {
	if len(patch) == 0 {
		return
	}
	e.write(events.NewActivityDeltaEvent(messageID, activityType, patch))
}

// ReasoningEncryptedValue emits REASONING_ENCRYPTED_VALUE.
func (e *Emitter) ReasoningEncryptedValue(subtype events.ReasoningEncryptedValueSubtype, entityID, encryptedValue string) {
	e.write(events.NewReasoningEncryptedValueEvent(subtype, entityID, encryptedValue))
}

// Custom emits CUSTOM with the given name and value.
func (e *Emitter) Custom(name string, value any) {
	e.write(events.NewCustomEvent(name, events.WithValue(value)))
}
