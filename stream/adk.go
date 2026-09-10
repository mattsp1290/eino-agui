package stream

import (
	"context"
	"errors"
	"fmt"
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
	Identity      AgenticStreamIdentity
	Blocks        BlockContextResolver
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

type AgentEventResult struct {
	Projections   []*convert.AgenticProjection
	Interrupts    []convert.PausedV1
	Cancellations []convert.CancelledV1
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
			return finish(fmt.Errorf("resolve agent event %d: %w", ordinal, err), true)
		}
		if resolution.Subagent != nil {
			candidate, err := projectSubagentLifecycle(event.AgentName, resolution)
			if err != nil {
				return finish(err, true)
			}
			result.Subagents = append(result.Subagents, candidate)
		}
		if event.Err != nil {
			var cancelled *adk.CancelError
			if errors.As(event.Err, &cancelled) {
				result.Cancellations = append(result.Cancellations, projectCancel(cancelled))
				return finish(nil, false)
			}
			return finish(event.Err, true)
		}
		if event.Output != nil {
			if event.Output.CustomizedOutput != nil {
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
				return finish(err, true)
			}
			result.Projections = append(result.Projections, projection)
		}
		if event.Action != nil {
			if event.Action.CustomizedAction != nil {
				return finish(errors.New("customized agent action has no public adapter"), true)
			}
			if event.Action.Interrupted != nil {
				paused, err := projectInterrupt(event.Action.Interrupted, config.limits.MaxInterruptTargets)
				if err != nil {
					return finish(err, true)
				}
				result.Interrupts = append(result.Interrupts, paused)
			}
			if event.Action.TransferToAgent != nil {
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
	if output.MessageStream != nil {
		defer output.MessageStream.Close()
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

func projectInterrupt(info *adk.InterruptInfo, maxTargets int) (convert.PausedV1, error) {
	if info == nil || len(info.InterruptContexts) == 0 || len(info.InterruptContexts) > maxTargets {
		return convert.PausedV1{}, errors.New("interrupt has no public targets")
	}
	out := convert.PausedV1{Targets: make([]convert.InterruptTargetV1, len(info.InterruptContexts))}
	seen := map[string]bool{}
	for i, interrupt := range info.InterruptContexts {
		if interrupt == nil {
			return convert.PausedV1{}, fmt.Errorf("interrupt target %d is nil", i)
		}
		address := interrupt.Address.String()
		if interrupt.ID == "" || address == "" || seen[interrupt.ID+"\x00"+address] {
			return convert.PausedV1{}, fmt.Errorf("interrupt target %d is invalid or duplicate", i)
		}
		seen[interrupt.ID+"\x00"+address] = true
		out.Targets[i] = convert.InterruptTargetV1{ID: interrupt.ID, Address: address}
	}
	return out, nil
}

func projectCancel(cancelled *adk.CancelError) convert.CancelledV1 {
	out := convert.CancelledV1{RequestedMode: "unknown", ObservedMode: "unknown", Classification: "safe_point"}
	if cancelled == nil || cancelled.Info == nil {
		return out
	}
	out.RequestedMode = fmt.Sprint(cancelled.Info.Mode)
	out.ObservedMode = out.RequestedMode
	if cancelled.Info.Escalated {
		out.ObservedMode = fmt.Sprint(adk.CancelImmediate)
		out.Classification = "escalated"
	}
	if cancelled.Info.Timeout {
		out.Classification = "timeout"
	}
	if cancelled.Info.Mode == adk.CancelImmediate {
		out.Classification = "immediate"
	}
	return out
}

func stringifyRunPath(path []adk.RunStep) string {
	parts := make([]string, len(path))
	for i := range path {
		parts[i] = path[i].String()
	}
	return strings.Join(parts, "/")
}
