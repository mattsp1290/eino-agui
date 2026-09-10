package stream

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
)

type AgentEventSource interface {
	Next(context.Context) (*adk.TypedAgentEvent[*schema.AgenticMessage], bool, error)
	Abort(error)
	Wait(context.Context) error
}

type iteratorEventSource struct {
	abort     func(error)
	wait      func(context.Context) error
	events    chan iteratorEvent
	done      chan struct{}
	stop      chan struct{}
	abortOnce sync.Once
}
type iteratorEvent struct {
	event *adk.TypedAgentEvent[*schema.AgenticMessage]
	ok    bool
}

func NewAgentEventSource(iterator *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]], abort func(error), wait func(context.Context) error) (AgentEventSource, error) {
	if iterator == nil || abort == nil || wait == nil {
		return nil, errors.New("agent event source requires iterator, abort, and wait hooks")
	}
	source := &iteratorEventSource{abort: abort, wait: wait, events: make(chan iteratorEvent, 1), done: make(chan struct{}), stop: make(chan struct{})}
	go func() {
		defer close(source.done)
		defer close(source.events)
		for {
			event, ok := iterator.Next()
			select {
			case source.events <- iteratorEvent{event: event, ok: ok}:
				if !ok {
					return
				}
			case <-source.stop:
				return
			}
		}
	}()
	return source, nil
}

func (s *iteratorEventSource) Next(ctx context.Context) (*adk.TypedAgentEvent[*schema.AgenticMessage], bool, error) {
	select {
	case item, ok := <-s.events:
		if !ok {
			return nil, false, nil
		}
		return item.event, item.ok, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}
func (s *iteratorEventSource) Abort(err error) {
	s.abortOnce.Do(func() {
		close(s.stop)
		s.abort(err)
	})
}
func (s *iteratorEventSource) Wait(ctx context.Context) error {
	if err := s.wait(ctx); err != nil {
		return err
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type AgentEventCoordinates struct {
	AgentName     string
	RunPath       string
	EventOrdinal  int
	OutputOrdinal int
}
type AgentEventResolution struct {
	Identity AgenticStreamIdentity
	Blocks   BlockContextResolver
	// PauseID and Correlation are host-owned inputs used only when the event is
	// a business interrupt. The bridge never derives either from checkpoint data.
	PauseID       string
	Correlation   *convert.ApprovalInterruptCorrelation
	ParentRunID   string
	SubagentRunID string
	Subagent      *SubagentLifecycleResolution
}
type AgentEventIdentityResolver interface {
	ResolveAgentEvent(AgentEventCoordinates) (AgentEventResolution, error)
}

type AgentControlKind string

const (
	AgentControlTransfer  AgentControlKind = "transfer"
	AgentControlExit      AgentControlKind = "exit"
	AgentControlBreakLoop AgentControlKind = "break_loop"
)

type AgentControlObservation struct {
	Kind        AgentControlKind
	Destination string
}

type SubagentLifecycleKind string

const (
	SubagentLifecycleStarted  SubagentLifecycleKind = "started"
	SubagentLifecycleFinished SubagentLifecycleKind = "finished"
	SubagentLifecycleError    SubagentLifecycleKind = "error"
)

// SubagentLifecycleResolution is an explicit host observation. DrainAgenticEvents
// never infers lifecycle boundaries merely because an ADK message has a child
// path: one event is not proof that the child started or settled.
type SubagentLifecycleResolution struct {
	Kind   SubagentLifecycleKind
	Detail string
}

type SubagentLifecycleCandidate struct {
	Kind          SubagentLifecycleKind
	Identity      AgenticStreamIdentity
	ParentRunID   string
	SubagentRunID string
	AgentName     string
	Detail        string
}

// CancellationCandidate binds a public cancellation observation to the exact
// host-resolved turn and attempt that it affects.
type CancellationCandidate struct {
	Identity     AgenticStreamIdentity
	Cancellation convert.CancelledV1
}

type AgentEventResult struct {
	Projections   []*convert.AgenticProjection
	Interrupts    []convert.PausedV1
	Cancellations []CancellationCandidate
	Controls      []AgentControlObservation
	Subagents     []SubagentLifecycleCandidate
	ObserverErr   error
	Partial       bool
}

type AgentEventOption func(*agentEventConfig)
type agentEventConfig struct {
	limits          convert.ProjectionLimits
	cleanupDeadline time.Duration
	sink            TransientSink
	cancellation    *CancellationCandidate
}

func WithAgentEventProjectionLimits(limits convert.ProjectionLimits) AgentEventOption {
	return func(c *agentEventConfig) { c.limits = limits }
}
func WithAgentEventCleanupDeadline(deadline time.Duration) AgentEventOption {
	return func(c *agentEventConfig) { c.cleanupDeadline = deadline }
}
func WithAgentEventTransientSink(sink TransientSink) AgentEventOption {
	return func(c *agentEventConfig) { c.sink = sink }
}

// WithAgentEventCancellationCandidate supplies the host-owned identity and
// policy classification to return if the execution context or a nested stream
// is cancelled. It does not cause or emit cancellation by itself.
func WithAgentEventCancellationCandidate(identity AgenticStreamIdentity, cancellation convert.CancelledV1) AgentEventOption {
	return func(c *agentEventConfig) {
		identity.AgentPath = append([]convert.AgentPathSegment(nil), identity.AgentPath...)
		c.cancellation = &CancellationCandidate{Identity: identity, Cancellation: cancellation}
	}
}

type CleanupContractError struct{ Err error }

func (e *CleanupContractError) Error() string {
	return "agent event source cleanup contract failed: " + e.Err.Error()
}
func (e *CleanupContractError) Unwrap() error { return e.Err }

func DrainAgenticEvents(ctx context.Context, source AgentEventSource, resolver AgentEventIdentityResolver, opts ...AgentEventOption) (*AgentEventResult, error) {
	if source == nil || resolver == nil {
		return nil, errors.New("agent event source and identity resolver are required")
	}
	config := agentEventConfig{limits: convert.DefaultProjectionLimits(), cleanupDeadline: 5 * time.Second}
	for _, option := range opts {
		if option != nil {
			option(&config)
		}
	}
	if config.cleanupDeadline <= 0 {
		return nil, errors.New("cleanup deadline must be positive")
	}
	if config.cancellation != nil {
		if err := validateAgenticStreamIdentity(config.cancellation.Identity, config.limits); err != nil {
			return nil, fmt.Errorf("execution cancellation identity: %w", err)
		}
		envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: convert.EnvelopeCancelled, Identity: streamIdentity(config.cancellation.Identity), Cancelled: &config.cancellation.Cancellation}
		if _, err := convert.LifecycleDigestV1(envelope); err != nil {
			return nil, fmt.Errorf("execution cancellation candidate: %w", err)
		}
	}
	result := &AgentEventResult{}
	finish := func(primary error, abort bool) (*AgentEventResult, error) {
		if abort {
			source.Abort(primary)
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), config.cleanupDeadline)
		defer cancel()
		if err := source.Wait(cleanupCtx); err != nil {
			cleanup := &CleanupContractError{Err: err}
			if primary != nil {
				primary = errors.Join(primary, cleanup)
			} else {
				primary = cleanup
			}
		}
		if primary != nil {
			result.Partial = true
		}
		return result, primary
	}
	for ordinal := 0; ; ordinal++ {
		event, ok, err := source.Next(ctx)
		if err != nil {
			appendConfiguredCancellation(result, config.cancellation, err)
			return finish(err, true)
		}
		if !ok {
			return finish(nil, false)
		}
		if event == nil {
			return finish(errors.New("agent event source returned a nil event"), true)
		}
		resolution, err := resolver.ResolveAgentEvent(AgentEventCoordinates{AgentName: event.AgentName, RunPath: stringifyRunPath(event.RunPath), EventOrdinal: ordinal})
		if err != nil {
			closeUndrainedAgentEventStream(event)
			return finish(fmt.Errorf("resolve agent event %d: %w", ordinal, err), true)
		}
		if err := validateAgenticStreamIdentity(resolution.Identity, config.limits); err != nil {
			closeUndrainedAgentEventStream(event)
			return finish(fmt.Errorf("resolve agent event %d identity: %w", ordinal, err), true)
		}
		if resolution.Subagent != nil {
			candidate, err := projectSubagentLifecycle(event.AgentName, resolution)
			if err != nil {
				closeUndrainedAgentEventStream(event)
				return finish(err, true)
			}
			result.Subagents = append(result.Subagents, candidate)
		}
		if event.Err != nil {
			closeUndrainedAgentEventStream(event)
			var cancelled *adk.CancelError
			if errors.As(event.Err, &cancelled) {
				cancellation, err := projectCancel(cancelled)
				if err != nil {
					return finish(fmt.Errorf("project cancellation: %w", err), true)
				}
				result.Cancellations = append(result.Cancellations, CancellationCandidate{Identity: resolution.Identity, Cancellation: cancellation})
				return finish(nil, false)
			}
			return finish(event.Err, true)
		}
		if event.Output != nil {
			if event.Output.CustomizedOutput != nil {
				closeUndrainedAgentEventStream(event)
				return finish(errors.New("customized agent output has no public adapter"), true)
			}
			if event.Output.MessageOutput == nil {
				return finish(errors.New("agent output has no message variant"), true)
			}
			projection, observerErr, err := drainAgentMessageOutput(ctx, event.Output.MessageOutput, resolution, config)
			if observerErr != nil && result.ObserverErr == nil {
				result.ObserverErr = observerErr
			}
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					if config.cancellation != nil && sameStreamIdentity(config.cancellation.Identity, resolution.Identity) {
						appendConfiguredCancellation(result, config.cancellation, err)
					}
				}
				return finish(err, true)
			}
			result.Projections = append(result.Projections, projection)
		}
		if event.Action != nil {
			if event.Action.CustomizedAction != nil {
				return finish(errors.New("customized agent action has no public adapter"), true)
			}
			if event.Action.Interrupted != nil {
				paused, err := projectInterrupt(event.Action.Interrupted, resolution.PauseID, resolution.Correlation, config.limits.MaxInterruptTargets)
				if err != nil {
					return finish(err, true)
				}
				envelope := &convert.AgenticEnvelopeV1{Version: convert.AgenticSchemaVersion, Kind: convert.EnvelopePaused, Identity: streamIdentity(resolution.Identity), Paused: &paused}
				if _, err := convert.LifecycleDigestV1(envelope); err != nil {
					return finish(fmt.Errorf("project interrupt: %w", err), true)
				}
				result.Interrupts = append(result.Interrupts, paused)
			}
			if event.Action.TransferToAgent != nil {
				if event.Action.TransferToAgent.DestAgentName == "" {
					return finish(errors.New("agent transfer destination is required"), true)
				}
				result.Controls = append(result.Controls, AgentControlObservation{Kind: AgentControlTransfer, Destination: event.Action.TransferToAgent.DestAgentName})
			}
			if event.Action.Exit {
				result.Controls = append(result.Controls, AgentControlObservation{Kind: AgentControlExit})
			}
			if event.Action.BreakLoop != nil {
				result.Controls = append(result.Controls, AgentControlObservation{Kind: AgentControlBreakLoop})
			}
		}
	}
}

func closeUndrainedAgentEventStream(event *adk.TypedAgentEvent[*schema.AgenticMessage]) {
	if event == nil || event.Output == nil || event.Output.MessageOutput == nil || event.Output.MessageOutput.MessageStream == nil {
		return
	}
	event.Output.MessageOutput.MessageStream.Close()
}

func appendConfiguredCancellation(result *AgentEventResult, candidate *CancellationCandidate, err error) {
	if candidate == nil || (!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)) || len(result.Cancellations) != 0 {
		return
	}
	copyCandidate := *candidate
	copyCandidate.Identity.AgentPath = append([]convert.AgentPathSegment(nil), candidate.Identity.AgentPath...)
	result.Cancellations = append(result.Cancellations, copyCandidate)
}

func sameStreamIdentity(left, right AgenticStreamIdentity) bool {
	return left.SessionID == right.SessionID && left.RunID == right.RunID && left.TurnID == right.TurnID && left.MessageID == right.MessageID && left.AttemptID == right.AttemptID && reflect.DeepEqual(left.AgentPath, right.AgentPath)
}

func projectSubagentLifecycle(agentName string, resolution AgentEventResolution) (SubagentLifecycleCandidate, error) {
	if resolution.SubagentRunID == "" || resolution.ParentRunID == "" || resolution.SubagentRunID == resolution.ParentRunID {
		return SubagentLifecycleCandidate{}, errors.New("subagent lifecycle requires distinct parent and subagent run IDs")
	}
	if resolution.Subagent.Kind != SubagentLifecycleStarted && resolution.Subagent.Kind != SubagentLifecycleFinished && resolution.Subagent.Kind != SubagentLifecycleError {
		return SubagentLifecycleCandidate{}, errors.New("subagent lifecycle kind is invalid")
	}
	path := resolution.Identity.AgentPath
	if len(path) < 2 || path[len(path)-1].RunID != resolution.SubagentRunID || path[len(path)-2].RunID != resolution.ParentRunID {
		return SubagentLifecycleCandidate{}, errors.New("subagent lifecycle does not match resolved agent path")
	}
	if agentName == "" || path[len(path)-1].Name != agentName {
		return SubagentLifecycleCandidate{}, errors.New("subagent lifecycle does not match agent name")
	}
	return SubagentLifecycleCandidate{
		Kind: resolution.Subagent.Kind, Identity: resolution.Identity,
		ParentRunID: resolution.ParentRunID, SubagentRunID: resolution.SubagentRunID,
		AgentName: agentName, Detail: resolution.Subagent.Detail,
	}, nil
}

func drainAgentMessageOutput(ctx context.Context, output *adk.TypedMessageVariant[*schema.AgenticMessage], resolution AgentEventResolution, config agentEventConfig) (*convert.AgenticProjection, error, error) {
	if output == nil {
		return nil, nil, errors.New("agent message output is nil")
	}
	streamOwnedByDrain := false
	if output.MessageStream != nil {
		defer func() {
			if !streamOwnedByDrain {
				output.MessageStream.Close()
			}
		}()
	}
	if output.AgenticRole != schema.AgenticRoleTypeAssistant && output.AgenticRole != schema.AgenticRoleTypeUser && output.AgenticRole != schema.AgenticRoleTypeSystem {
		return nil, nil, errors.New("agent message output has invalid role")
	}
	if resolution.Blocks == nil {
		return nil, nil, errors.New("agent event block resolver is required")
	}
	if output.IsStreaming {
		if output.Message != nil || output.MessageStream == nil {
			return nil, nil, errors.New("streaming message variant must contain only a stream")
		}
		streamConfig := agenticConfig{limits: config.limits, sink: config.sink, expectedRole: output.AgenticRole}
		streamOwnedByDrain = true
		result, err := drainAgenticReader(ctx, output.MessageStream, resolution.Identity, resolution.Blocks, streamConfig)
		if err != nil {
			return nil, result.ObserverErr, err
		}
		if result.Assistant == nil || result.Assistant.Role != output.AgenticRole {
			return nil, result.ObserverErr, errors.New("streamed message role does not match event role")
		}
		return result.PublicProjection, result.ObserverErr, nil
	}
	if output.Message == nil || output.MessageStream != nil {
		return nil, nil, errors.New("complete message variant must contain only a message")
	}
	if output.Message.Role != output.AgenticRole {
		return nil, nil, errors.New("complete message role does not match event role")
	}
	contexts := make([]convert.AgenticBlockContext, len(output.Message.ContentBlocks))
	for index, block := range output.Message.ContentBlocks {
		if block == nil {
			return nil, nil, errors.New("complete message contains nil block")
		}
		context, err := resolution.Blocks.ResolveBlock(index, block.Type)
		if err != nil {
			return nil, nil, err
		}
		contexts[index] = context
	}
	projection, err := convert.ToAgenticProjection(output.Message, convert.AgenticProjectionContext{Identity: streamIdentity(resolution.Identity), Blocks: contexts, Limits: config.limits})
	return projection, nil, err
}

func projectInterrupt(info *adk.InterruptInfo, pauseID string, correlation *convert.ApprovalInterruptCorrelation, maxTargets int) (convert.PausedV1, error) {
	if pauseID == "" {
		return convert.PausedV1{}, errors.New("interrupt requires a host-assigned pause ID")
	}
	if info == nil || len(info.InterruptContexts) == 0 || len(info.InterruptContexts) > maxTargets {
		return convert.PausedV1{}, errors.New("interrupt has no public targets")
	}
	out := convert.PausedV1{PauseID: pauseID, Targets: make([]convert.InterruptTargetV1, len(info.InterruptContexts))}
	if correlation != nil {
		copyCorrelation := *correlation
		out.Correlation = &copyCorrelation
	}
	seenIDs := make(map[string]struct{}, len(info.InterruptContexts))
	seenAddresses := make(map[string]struct{}, len(info.InterruptContexts))
	for i, interrupt := range info.InterruptContexts {
		if interrupt == nil {
			return convert.PausedV1{}, fmt.Errorf("interrupt target %d is nil", i)
		}
		address := interrupt.Address.String()
		if interrupt.ID == "" || address == "" {
			return convert.PausedV1{}, fmt.Errorf("interrupt target %d is invalid or duplicate", i)
		}
		if _, duplicate := seenIDs[interrupt.ID]; duplicate {
			return convert.PausedV1{}, fmt.Errorf("interrupt target %d is invalid or duplicate", i)
		}
		if _, duplicate := seenAddresses[address]; duplicate {
			return convert.PausedV1{}, fmt.Errorf("interrupt target %d is invalid or duplicate", i)
		}
		seenIDs[interrupt.ID] = struct{}{}
		seenAddresses[address] = struct{}{}
		out.Targets[i] = convert.InterruptTargetV1{ID: interrupt.ID, Address: address}
	}
	return out, nil
}

func projectCancel(cancelled *adk.CancelError) (convert.CancelledV1, error) {
	if cancelled == nil || cancelled.Info == nil {
		return convert.CancelledV1{}, errors.New("cancellation info is required")
	}
	requested, ok := cancelModeName(cancelled.Info.Mode)
	if !ok {
		return convert.CancelledV1{}, errors.New("cancellation mode is invalid")
	}
	if requested == convert.CancellationModeImmediate && (cancelled.Info.Escalated || cancelled.Info.Timeout) {
		return convert.CancelledV1{}, errors.New("immediate cancellation cannot be escalated")
	}
	out := convert.CancelledV1{RequestedMode: requested, ObservedMode: requested, Classification: convert.CancellationClassSafePoint}
	out.ObservedMode = out.RequestedMode
	if cancelled.Info.Escalated || cancelled.Info.Timeout {
		out.ObservedMode = convert.CancellationModeImmediate
		out.Classification = convert.CancellationClassEscalated
	}
	if cancelled.Info.Timeout {
		out.Classification = convert.CancellationClassTimeout
	}
	if cancelled.Info.Mode == adk.CancelImmediate {
		out.Classification = convert.CancellationClassImmediate
	}
	return out, nil
}

func cancelModeName(mode adk.CancelMode) (convert.CancellationModeV1, bool) {
	switch mode {
	case adk.CancelImmediate:
		return convert.CancellationModeImmediate, true
	case adk.CancelAfterChatModel:
		return convert.CancellationModeAfterChatModel, true
	case adk.CancelAfterToolCalls:
		return convert.CancellationModeAfterToolCalls, true
	case adk.CancelAfterChatModel | adk.CancelAfterToolCalls:
		return convert.CancellationModeAfterChatOrToolCalls, true
	default:
		return "", false
	}
}

func stringifyRunPath(path []adk.RunStep) string {
	parts := make([]string, len(path))
	for i := range path {
		parts[i] = path[i].String()
	}
	return strings.Join(parts, "/")
}
