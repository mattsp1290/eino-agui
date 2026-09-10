package emitter

import (
	"bufio"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"

	"github.com/mattsp1290/eino-agui/convert"
)

type DeliveryMode string

const (
	DeliveryModeLiveContinuation DeliveryMode = "live_continuation"
	DeliveryModeCommittedOnly    DeliveryMode = "committed_only"
	DeliveryModeReplay           DeliveryMode = "replay"
)

// ObserverSink adapts an observer-only Emitter to the transient sink contract
// used by the stream package without coupling either package to the other.
// Detach disables only this observer; it never cancels host execution.
type ObserverSink struct {
	emitter   *Emitter
	detached  bool
	detachErr error
}

type committedPause struct {
	identity    convert.AgenticIdentityV1
	targets     []convert.InterruptTargetV1
	correlation *convert.ApprovalInterruptCorrelation
}

// NewObserverSink adapts an Emitter created by NewObserverEmitter for use with
// stream.WithTransientSink or stream.WithAgentEventTransientSink.
func NewObserverSink(emitter *Emitter) *ObserverSink {
	return &ObserverSink{emitter: emitter}
}

// Emit writes one transient event and translates the Emitter's bool/error
// surface into the stream sink's error surface.
func (s *ObserverSink) Emit(event events.Event) error {
	if s == nil || s.emitter == nil {
		return errors.New("observer emitter is required")
	}
	if s.detached {
		if s.detachErr != nil {
			return s.detachErr
		}
		return errors.New("observer sink is detached")
	}
	if err := s.emitter.ctx.Err(); err != nil {
		return err
	}
	if s.emitter.Emit(event) {
		return nil
	}
	if err := s.emitter.Err(); err != nil {
		return err
	}
	if err := s.emitter.EncErr(); err != nil {
		return err
	}
	return errors.New("observer event was not emitted")
}

// Detach records the first observer failure and disables later writes.
func (s *ObserverSink) Detach(err error) {
	if s == nil || s.detached {
		return
	}
	s.detached = true
	s.detachErr = err
}

// Err returns the error that detached this observer, if any.
func (s *ObserverSink) Err() error {
	if s == nil {
		return nil
	}
	return s.detachErr
}

// NewObserverEmitter creates a transport observer with no execution-cancel
// callback. A failed observer detaches without changing host execution state.
func NewObserverEmitter(ctx context.Context, w *bufio.Writer, sw *sse.SSEWriter) *Emitter {
	return NewEmitter(ctx, w, sw, "", "", nil)
}

func (e *Emitter) EmitTransientBlock(block convert.TransientBlock) bool {
	if block.Event == nil || !block.Identity.Transient {
		e.recordEncodingError(errors.New("transient block requires a transient identity and event"))
		return false
	}
	metadataIdentity, ok := block.Event.GetBaseEvent().Metadata[convert.AgenticCustomEventName]
	if !ok || !reflect.DeepEqual(metadataIdentity, block.Identity) {
		e.recordEncodingError(errors.New("transient event identity mismatch"))
		return false
	}
	if !e.prevalidate([]events.Event{block.Event}) {
		return false
	}
	if !e.allowAgenticOutput(block.Identity) {
		return false
	}
	if !e.Emit(block.Event) {
		return false
	}
	e.recordAgenticOutput(block.Identity)
	return true
}

func (e *Emitter) EmitCommittedProjection(projection *convert.AgenticProjection, receipt convert.CommitReceiptV1, mode DeliveryMode) bool {
	if projection == nil || projection.Public == nil {
		e.recordEncodingError(errors.New("committed projection is required"))
		return false
	}
	if err := convert.ValidateCommitReceipt(receipt, projection.Public, nil); err != nil {
		e.recordEncodingError(err)
		return false
	}
	if !e.allowAgenticReceipt(receipt) {
		return false
	}
	if mode != DeliveryModeLiveContinuation && mode != DeliveryModeCommittedOnly && mode != DeliveryModeReplay {
		e.recordEncodingError(errors.New("unknown agentic delivery mode"))
		return false
	}
	if len(projection.Blocks) != len(projection.Public.ContentBlocks) {
		e.recordEncodingError(errors.New("agentic projection block cache does not match public projection"))
		return false
	}
	if !e.allowAgenticOutput(projection.Public.Identity) {
		return false
	}
	groups := make([][]events.Event, len(projection.Blocks))
	for i := range projection.Blocks {
		block := &projection.Blocks[i]
		if !reflect.DeepEqual(block.Public, projection.Public.ContentBlocks[i]) {
			e.recordEncodingError(errors.New("agentic projection block cache does not match public projection"))
			return false
		}
		envelope := convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: convert.EnvelopeContentBlock, Identity: block.Public.Identity, ContentBlock: &block.Public}
		digest, err := convert.LifecycleDigestV1(&envelope)
		if err != nil {
			e.recordEncodingError(err)
			return false
		}
		envelope.Digest = digest
		custom := events.NewCustomEvent(convert.AgenticCustomEventName, events.WithValue(&envelope))
		custom.GetBaseEvent().Metadata = map[string]any{convert.AgenticCustomEventName: block.Public.Identity}
		group := []events.Event{custom}
		if mode != DeliveryModeLiveContinuation {
			native, err := convert.CommittedNativeEvents(block.Public)
			if err != nil {
				e.recordEncodingError(err)
				return false
			}
			group = append(native, custom)
		}
		groups[i] = group
	}
	if projection.Public.ResponseMeta != nil {
		envelope := convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: convert.EnvelopeResponseMeta, Identity: projection.Public.Identity, ResponseMeta: projection.Public.ResponseMeta}
		digest, err := convert.LifecycleDigestV1(&envelope)
		if err != nil {
			e.recordEncodingError(err)
			return false
		}
		envelope.Digest = digest
		custom := events.NewCustomEvent(convert.AgenticCustomEventName, events.WithValue(&envelope))
		custom.GetBaseEvent().Metadata = map[string]any{convert.AgenticCustomEventName: envelope.Identity}
		group := []events.Event{custom}
		groups = append(groups, group)
	}
	if !e.emitAgenticGroups(groups) {
		return false
	}
	e.recordAgenticOutput(projection.Public.Identity)
	e.recordAgenticReceipt(receipt)
	return true
}

func (e *Emitter) TurnStarted(input convert.TurnStartedV1, receipt convert.CommitReceiptV1) bool {
	return e.emitLifecycle(convert.EnvelopeTurnStarted, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input })
}
func (e *Emitter) TurnFinished(input convert.TurnFinishedV1, receipt convert.CommitReceiptV1) bool {
	return e.emitLifecycle(convert.EnvelopeTurnFinished, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input })
}
func (e *Emitter) AttemptReplaced(input convert.AttemptReplacedV1, receipt convert.CommitReceiptV1) bool {
	key := agenticMessageKey(receipt.Identity)
	if key == "" {
		e.recordEncodingError(errors.New("attempt replacement requires an owning message identity"))
		return false
	}
	if current := e.agenticAttempt(key); current != "" && current != input.OldAttemptID {
		e.recordEncodingError(errors.New("attempt replacement old attempt does not match emitted output"))
		return false
	}
	if !e.emitLifecycle(convert.EnvelopeAttemptReplaced, receipt, func(v *convert.AgenticEnvelopeV1) { v.AttemptReplaced = &input }) {
		return false
	}
	e.setAgenticAttempt(key, input.NewAttemptID)
	return true
}
func (e *Emitter) Paused(input convert.PausedV1, receipt convert.CommitReceiptV1) bool {
	key := agenticPauseKey(receipt.Identity, input.PauseID)
	if _, exists := e.agenticPauses[key]; exists {
		e.recordEncodingError(errors.New("pause ID has already been committed for this run"))
		return false
	}
	if !e.emitLifecycle(convert.EnvelopePaused, receipt, func(v *convert.AgenticEnvelopeV1) { v.Paused = &input }) {
		return false
	}
	if e.agenticPauses == nil {
		e.agenticPauses = make(map[string]committedPause)
	}
	var correlation *convert.ApprovalInterruptCorrelation
	if input.Correlation != nil {
		copyCorrelation := *input.Correlation
		correlation = &copyCorrelation
	}
	e.agenticPauses[key] = committedPause{identity: receipt.Identity, targets: append([]convert.InterruptTargetV1(nil), input.Targets...), correlation: correlation}
	return true
}
func (e *Emitter) Resumed(input convert.ResumedV1, receipt convert.CommitReceiptV1) bool {
	key := agenticPauseKey(receipt.Identity, input.PauseID)
	pause, ok := e.agenticPauses[key]
	if !ok {
		e.recordEncodingError(errors.New("resume requires a preceding committed pause"))
		return false
	}
	if !reflect.DeepEqual(pause.identity, receipt.Identity) {
		e.recordEncodingError(errors.New("resume identity does not match the committed pause"))
		return false
	}
	remaining, err := resumedTargets(pause.targets, input.Targets, input.Full)
	if err != nil {
		e.recordEncodingError(err)
		return false
	}
	correlationResumed, err := validateResumeCorrelation(pause.correlation, input.Correlation, input.Targets)
	if err != nil {
		e.recordEncodingError(err)
		return false
	}
	if !e.emitLifecycle(convert.EnvelopeResumed, receipt, func(v *convert.AgenticEnvelopeV1) { v.Resumed = &input }) {
		return false
	}
	if len(remaining) == 0 {
		delete(e.agenticPauses, key)
	} else {
		pause.targets = remaining
		if correlationResumed {
			pause.correlation = nil
		}
		e.agenticPauses[key] = pause
	}
	return true
}
func (e *Emitter) Cancelled(input convert.CancelledV1, receipt convert.CommitReceiptV1) bool {
	return e.emitLifecycle(convert.EnvelopeCancelled, receipt, func(v *convert.AgenticEnvelopeV1) { v.Cancelled = &input })
}
func (e *Emitter) RunStartedCommitted(input convert.LifecycleFactV1, receipt convert.CommitReceiptV1) bool {
	return e.emitLifecycleWithNative(convert.EnvelopeRunStarted, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input }, events.NewRunStartedEvent(receipt.Identity.ThreadID, receipt.Identity.RunID))
}
func (e *Emitter) RunFinishedCommitted(input convert.LifecycleFactV1, receipt convert.CommitReceiptV1) bool {
	if !input.LoopSettled {
		e.recordEncodingError(errors.New("run finish requires loopSettled"))
		return false
	}
	return e.emitLifecycleWithNative(convert.EnvelopeRunFinished, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input }, events.NewRunFinishedEventWithOptions(receipt.Identity.ThreadID, receipt.Identity.RunID, events.WithSuccessOutcome()))
}
func (e *Emitter) RunErrorCommitted(input convert.LifecycleFactV1, receipt convert.CommitReceiptV1) bool {
	if !input.LoopSettled {
		e.recordEncodingError(errors.New("run error requires loopSettled"))
		return false
	}
	return e.emitLifecycleWithNative(convert.EnvelopeRunError, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input }, events.NewRunErrorEvent(input.Detail, events.WithRunID(receipt.Identity.RunID)))
}

func (e *Emitter) SubagentStartedCommitted(input convert.LifecycleFactV1, receipt convert.CommitReceiptV1) bool {
	runID, name, ok := subagentIdentity(receipt.Identity)
	if !ok {
		e.recordEncodingError(errors.New("subagent identity requires a nested agent path"))
		return false
	}
	return e.emitLifecycleWithNative(convert.EnvelopeSubagentStarted, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input }, events.NewSubagentStartedEvent(runID, name))
}

func (e *Emitter) SubagentFinishedCommitted(input convert.LifecycleFactV1, receipt convert.CommitReceiptV1) bool {
	runID, _, ok := subagentIdentity(receipt.Identity)
	if !ok {
		e.recordEncodingError(errors.New("subagent identity requires a nested agent path"))
		return false
	}
	return e.emitLifecycleWithNative(convert.EnvelopeSubagentFinished, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input }, events.NewSubagentFinishedEvent(runID, events.WithSubagentSuccessOutcome()))
}

func (e *Emitter) SubagentErrorCommitted(input convert.LifecycleFactV1, receipt convert.CommitReceiptV1) bool {
	runID, _, ok := subagentIdentity(receipt.Identity)
	if !ok {
		e.recordEncodingError(errors.New("subagent identity requires a nested agent path"))
		return false
	}
	return e.emitLifecycleWithNative(convert.EnvelopeSubagentError, receipt, func(v *convert.AgenticEnvelopeV1) { v.Lifecycle = &input }, events.NewSubagentErrorEvent(runID, input.Detail))
}

func (e *Emitter) emitLifecycle(kind convert.AgenticEnvelopeKind, receipt convert.CommitReceiptV1, set func(*convert.AgenticEnvelopeV1)) bool {
	return e.emitLifecycleWithNative(kind, receipt, set)
}

func (e *Emitter) emitLifecycleWithNative(kind convert.AgenticEnvelopeKind, receipt convert.CommitReceiptV1, set func(*convert.AgenticEnvelopeV1), native ...events.Event) bool {
	envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: kind, Identity: receipt.Identity}
	set(envelope)
	digest, err := convert.LifecycleDigestV1(envelope)
	if err != nil {
		e.recordEncodingError(err)
		return false
	}
	envelope.Digest = digest
	if err := convert.ValidateCommitReceipt(receipt, nil, envelope); err != nil {
		e.recordEncodingError(err)
		return false
	}
	if !e.allowAgenticReceipt(receipt) {
		return false
	}
	event := events.NewCustomEvent(convert.AgenticCustomEventName, events.WithValue(envelope))
	event.GetBaseEvent().Metadata = map[string]any{convert.AgenticCustomEventName: envelope.Identity}
	group := append(append([]events.Event(nil), native...), event)
	for _, item := range group {
		item.GetBaseEvent().Metadata = map[string]any{convert.AgenticCustomEventName: envelope.Identity}
	}
	if !e.emitAgenticGroups([][]events.Event{group}) {
		return false
	}
	e.recordAgenticReceipt(receipt)
	return true
}

func subagentIdentity(identity convert.AgenticIdentityV1) (string, string, bool) {
	if len(identity.AgentPath) < 2 {
		return "", "", false
	}
	segment := identity.AgentPath[len(identity.AgentPath)-1]
	return segment.RunID, segment.Name, segment.RunID != "" && segment.Name != ""
}

func (e *Emitter) prevalidate(group []events.Event) bool {
	for _, event := range group {
		if event == nil {
			e.recordEncodingError(errors.New("cannot emit a nil AG-UI event"))
			return false
		}
		if err := event.Validate(); err != nil {
			e.recordEncodingError(err)
			return false
		}
		if _, err := event.ToJSON(); err != nil {
			e.recordEncodingError(err)
			return false
		}
	}
	return true
}

func (e *Emitter) emitAgenticGroups(groups [][]events.Event) bool {
	for _, group := range groups {
		if !e.prevalidate(group) {
			return false
		}
	}
	for _, group := range groups {
		for _, event := range group {
			if !e.Emit(event) {
				return false
			}
		}
	}
	return true
}

func (e *Emitter) recordEncodingError(err error) {
	if err != nil && e.encErr == nil {
		e.encErr = err
	}
}

func (e *Emitter) allowAgenticOutput(identity convert.AgenticIdentityV1) bool {
	key := agenticMessageKey(identity)
	if key == "" {
		e.recordEncodingError(errors.New("agentic output requires an owning message identity"))
		return false
	}
	if current := e.agenticAttempt(key); current != "" && current != identity.AttemptID {
		e.recordEncodingError(errors.New("successor attempt output requires a preceding committed replacement fact"))
		return false
	}
	return true
}

func (e *Emitter) recordAgenticOutput(identity convert.AgenticIdentityV1) {
	e.setAgenticAttempt(agenticMessageKey(identity), identity.AttemptID)
}

func (e *Emitter) agenticAttempt(key string) string {
	if e.agenticAttempts == nil {
		return ""
	}
	return e.agenticAttempts[key]
}

func (e *Emitter) setAgenticAttempt(key, attemptID string) {
	if key == "" || attemptID == "" {
		return
	}
	if e.agenticAttempts == nil {
		e.agenticAttempts = make(map[string]string)
	}
	e.agenticAttempts[key] = attemptID
}

func agenticMessageKey(identity convert.AgenticIdentityV1) string {
	if identity.SessionID == "" || identity.RunID == "" || identity.TurnID == "" || identity.MessageID == "" {
		return ""
	}
	var key strings.Builder
	for _, value := range []string{identity.SessionID, identity.RunID, identity.TurnID, identity.MessageID} {
		writeAgenticKeyPart(&key, value)
	}
	for _, segment := range identity.AgentPath {
		writeAgenticKeyPart(&key, segment.Name)
		writeAgenticKeyPart(&key, segment.RunID)
	}
	return key.String()
}

func agenticPauseKey(identity convert.AgenticIdentityV1, pauseID string) string {
	if identity.SessionID == "" || identity.RunID == "" || pauseID == "" {
		return ""
	}
	var key strings.Builder
	for _, value := range []string{identity.SessionID, identity.RunID, pauseID} {
		writeAgenticKeyPart(&key, value)
	}
	return key.String()
}

func (e *Emitter) allowAgenticReceipt(receipt convert.CommitReceiptV1) bool {
	if _, exists := e.agenticReceipts[agenticReceiptKey(receipt)]; exists {
		e.recordEncodingError(errors.New("commit receipt has already been emitted"))
		return false
	}
	return true
}

func (e *Emitter) recordAgenticReceipt(receipt convert.CommitReceiptV1) {
	if e.agenticReceipts == nil {
		e.agenticReceipts = make(map[string]struct{})
	}
	e.agenticReceipts[agenticReceiptKey(receipt)] = struct{}{}
}

func agenticReceiptKey(receipt convert.CommitReceiptV1) string {
	identity := receipt.Identity
	var key strings.Builder
	for _, value := range []string{
		receipt.Revision, receipt.Domain, string(receipt.Kind), string(receipt.Digest),
		identity.SessionID, identity.ThreadID, identity.RunID, identity.TurnID,
		identity.MessageID, identity.BlockID, identity.AttemptID, identity.CallID,
	} {
		writeAgenticKeyPart(&key, value)
	}
	for _, segment := range identity.AgentPath {
		writeAgenticKeyPart(&key, segment.Name)
		writeAgenticKeyPart(&key, segment.RunID)
	}
	return key.String()
}

func writeAgenticKeyPart(key *strings.Builder, value string) {
	key.WriteString(strconv.Itoa(len(value)))
	key.WriteByte(':')
	key.WriteString(value)
}

func resumedTargets(paused, resumed []convert.InterruptTargetV1, full bool) ([]convert.InterruptTargetV1, error) {
	if full && !reflect.DeepEqual(paused, resumed) {
		return nil, errors.New("full resume targets must exactly match the committed pause")
	}
	if !full && len(resumed) >= len(paused) {
		return nil, errors.New("partial resume must select a strict subset of committed pause targets")
	}
	remaining := append([]convert.InterruptTargetV1(nil), paused...)
	position := 0
	for _, target := range resumed {
		found := -1
		for i := position; i < len(paused); i++ {
			if paused[i] == target {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, errors.New("resume targets must be an ordered subset of the committed pause")
		}
		position = found + 1
		for i, candidate := range remaining {
			if candidate == target {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	return remaining, nil
}

func validateResumeCorrelation(paused, resumed *convert.ApprovalInterruptCorrelation, targets []convert.InterruptTargetV1) (bool, error) {
	if paused == nil {
		if resumed != nil {
			return false, errors.New("resume correlation was not present on the committed pause")
		}
		return false, nil
	}
	selected := false
	for _, target := range targets {
		if target.ID == paused.InterruptTargetID && target.Address == paused.InterruptAddress {
			selected = true
			break
		}
	}
	if selected && !reflect.DeepEqual(paused, resumed) {
		return false, errors.New("resume correlation must match the committed pause")
	}
	if !selected && resumed != nil {
		return false, errors.New("resume correlation target is not being resumed")
	}
	return selected, nil
}
