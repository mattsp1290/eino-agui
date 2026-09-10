# W4 — Typed ADK lifecycle projection

Goal: drain typed `adk.TypedAgentEvent[*schema.AgenticMessage]` observations into identified transient output and final candidates that the host can emit after commit, without owning ADK execution or persistence. Prerequisites: W1–W3.

## Evidence and constraints

- `TypedAgentEvent` exposes `AgentName`, `RunPath`, `Output`, `Action` and `Err`.
- `TypedMessageVariant` contains either a complete `AgenticMessage` or an exclusive `StreamReader[*schema.AgenticMessage]`; `AgenticRole` is available before stream consumption.
- ADK business interrupts expose structured `InterruptContexts` and target IDs/addresses. Checkpoint and arbitrary interrupt data are private host state.
- Eino v0.9.19 explicitly states that typed agentic agents do not yet wire model-stream cancellation monitoring or retry. This package reports events supplied by the host/ADK; it does not guarantee cancellation mechanics.
- A single typed event does not prove an agent start, finish, durable turn completion or enclosing run termination. Those boundaries require explicit host input.

## Proposed change surface

- `stream/adk.go` — **new** under existing `stream/`: `AgentEventSource`, `DrainAgenticEvents`, typed event classification and exclusive nested stream consumption.
- `stream/adk_test.go` — **new** under existing `stream/`: real async iterator, complete/streamed outputs, actions, errors and closure.
- `emitter/adk.go` — **new** under existing `emitter/`: typed, host-confirmed lifecycle input structs and native/custom mapping.
- `emitter/adk_test.go` — **new** under existing `emitter/`: run/turn/subagent/pause/cancel/attempt ordering.
- `internal/testmodel/adk.go` — **new** under existing `internal/testmodel/`: deterministic typed agent/iterator fixture using actual ADK constructors.
- `examples/agentic/main.go` — **new** under existing `examples/`: credential-free two-turn lifecycle demonstration with one enclosing run.

Proposed stream API:

```go
type AgentEventSource interface {
    Next(context.Context) (*adk.TypedAgentEvent[*schema.AgenticMessage], bool, error)
    Abort(error)
    Wait(context.Context) error
}

func NewAgentEventSource(
    iterator *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]],
    abort func(error),
    wait func(context.Context) error,
) (AgentEventSource, error)

func DrainAgenticEvents(
    ctx context.Context,
    source AgentEventSource,
    resolver AgentEventIdentityResolver,
    opts ...AgentEventOption,
) (*AgentEventResult, error)
```

`AgentEventIdentityResolver` accepts the framework `AgentName`, stringified `RunPath`, event ordinal, output ordinal and optional block index. It returns the host's base identity, complete block context, explicit parent-child subagent run IDs and applicable shared limits. Resolution must not derive durable IDs or provider/approval context from mutable agent display names.

`AgentEventSource` is the ownership boundary around Eino's raw blocking `AsyncIterator`, which has no close method or context-aware `Next`. `NewAgentEventSource` may own one receive worker only when the caller supplies an idempotent abort hook connected to the same runner/agent cancellation path and a wait hook that joins the producer; abort must unblock in-flight receive. The returned source joins its worker and producer before successful close. The bridge must not spawn an unowned receive goroutine around a raw iterator. Validate all hooks before draining. On execution-context cancellation or source failure call `Abort` once, wait with the configured positive cleanup deadline, close any nested reader, and return the primary error plus a typed cleanup error when join fails. Observer transport failure only detaches that sink. A source that cannot satisfy this contract fails the W4 stop/go gate.

`AgentEventOption` configures limits, cleanup deadline and optional detachable `TransientSink`. The drain returns final candidate observations; the host then commits them and calls W2's committed-emission helpers with a matching receipt. Turn and enclosing-run settlement remain explicit post-drain inputs, never callbacks invoked inside the raw ADK event loop.

The `ctx` argument is the host-owned execution context, not an SSE/watch request context. An observer sink error calls only `Detach`, records `ObserverErr` and disables the sink. It never invokes `AgentEventSource.Abort`, stops the drain or produces a cancellation candidate. Only cancellation of the execution context or an explicit host cancellation input calls `Abort` and may yield a candidate cancellation after the producer joins.

## Event mapping

- Complete message output: validate `AgenticRole`, project through W1, and return a final candidate without emitting its authoritative block/result facts.
- Streaming message output: require `IsStreaming`, a non-nil exclusive reader and no complete message. Drain via the W3 state machine variant that accepts an already-open reader; close once.
- Invalid message variant combinations fail deterministically: nil output, both message and stream, neither with `IsStreaming`, role mismatch or nil stream.
- Agent path transition: transient `SUBAGENT_STARTED` may be emitted from an explicit resolver boundary. Return candidate `SUBAGENT_FINISHED`/`SUBAGENT_ERROR` facts for post-commit emission. Carry parent run ID and stable subagent run ID supplied by the host.
- Business interrupt: return bounded candidate interrupt records. Keep each ADK address/ID distinct. An optional MCP approval correlation is accepted only as a separate host-validated pair containing native approval request ID and ADK target ID.
- `*adk.CancelError`, stream cancellation and context cancellation: return a candidate `cancelled` fact with requested/observed mode, graceful/immediate/timeout classification and affected attempt/turn identity supplied or verified by the host. Emit it only after host commit. Do not treat cancellation as approval or success.
- Ordinary execution error: close nested readers and return partial state. The host chooses a post-commit `SUBAGENT_ERROR` or enclosing `RUN_ERROR` from its ownership boundary. Observer errors are reported separately and are not execution errors.
- Customized output/action: reject by default. Add a future named public adapter only through a versioned contract; never pass `any` through.
- Transfer, exit and break-loop actions are host control observations. They do not by themselves emit run termination and are not public envelope kinds in v1; return them as typed host-facing observations for an explicit later contract decision.

## Run, turn, pause and attempt state model

```text
run_started
  -> turn_started
     -> attempt output*
     -> attempt_replaced -> successor attempt output*   (optional)
     -> turn_finished                                    (host says committed)
  -> turn_started -> ...                                 (zero or more later turns)
  -> paused                                              (committed, nonterminal, resumable)
  -> resumed -> turn_started -> ...                      (committed resume link, optional)
  -> run_finished | run_error                            (host says loop settled)
```

Immediate cancellation closes the affected attempt and returns partial state. Graceful cancellation reports the safe point the host/ADK actually reached. Timeout escalation reports both requested graceful mode and observed immediate termination. Pause is nonterminal and excludes checkpoint bytes. `ResumedV1` links prior pause identity, exact full/partial resumed ADK targets, optional separate MCP approval correlation, original run identity and new turn/attempt IDs. A normal turn completion never closes the run.

Attempt replacement requires old/new attempt IDs, cause, owning turn/message and replacement semantics. It must precede any successor output. Old partial content stays addressable for clients that need audit, but the final projection names exactly one authoritative attempt per block/message.

## Verification and acceptance

- Build typed events with actual `adk.EventFromAgenticMessage`, `adk.TypedInterrupt`/structured fixtures, `adk.AsyncIterator`, `adk.CancelError`-producing cancellation fixtures and real Eino stream readers.
- Run two successful normal turns under one run. Assert separate turn completion, no run terminal after the first turn, and exactly one final enclosing terminal event after explicit settlement.
- Test root plus nested agent paths, sibling agents, repeated names, parent-child IDs and subagent errors without identity collision.
- Test one complete output and one streaming output, iterator error after output, consumer cancellation, reader error and early transport failure. Count source abort/wait and reader cleanup exactly once.
- Test a producer blocked before its next event, cancellation while receive is blocked, transport failure and an adapter that fails to honor abort. The first three must join cleanly; the last must return a cleanup-contract error and keep the stop/go gate failed rather than claiming safe operation.
- Test observer failure/detach while receive is blocked without calling source abort; then publish committed replay to a new sink. Test explicit execution cancellation separately and assert abort/join exactly once and one cancellation candidate.
- Test partial attempt replacement and reject successor output that arrives before the replacement fact.
- Test committed pause → committed full and partial multi-target resume → new turn/attempt ordering with one separately correlated MCP approval ID. Reject swapped, duplicate and inferred correlations and omit checkpoint/private data.
- Test host commit failure after complete message, tool result, pause, cancellation and subagent completion produces no authoritative bytes; only a valid receipt permits emission.
- Test immediate, graceful, safe-point and timeout-escalated cancellation projections without claiming the bridge caused the upstream behavior.
- Compare final live `AgentEventResult` projection with W5's durable consumer fixture.
- Run `go test ./stream ./emitter ./internal/testmodel` and focused `go test -race -count=10 ./stream ./emitter`.

Exclusions: constructing TurnLoop, saving/loading checkpoints, resume authorization, deciding cancellation mode, executing an approval or committing turn/run state.
