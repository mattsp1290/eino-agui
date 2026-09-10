# W3 — AgenticModel streaming and partial attempts

Goal: consume one Eino v0.9.19 `model.AgenticModel` stream, emit identified transient native deltas, and return a complete candidate or partial state with exact cleanup behavior. Prerequisites: W1–W2.

## Evidence and existing patterns

- `stream.StreamTurn` checks context and emitter transport errors before each receive, defers reader close and returns non-nil partial results after stream open.
- `stream.turnState` currently handles a classic message grammar and keeps all chunks for `schema.ConcatMessages`.
- Eino `schema.ContentBlock.StreamingMeta.Index` identifies the final block position. `schema.ConcatAgenticMessages` validates and concatenates indexed chunks across all 20 variants.
- `schema.StreamReader` supports `WithOnEOF`, so the final receive can yield an injected value or error before terminal EOF.

## Proposed change surface

- `stream/agentic.go` — **new** under existing `stream/`: exported `StreamAgenticTurn`, options, result and identity resolver interfaces.
- `stream/agentic_state.go` — **new** under existing `stream/`: block-index state machine, native/custom emission coordination and finalization.
- `stream/agentic_blocks.go` — **new** under existing `stream/`: per-kind incremental correlation without provider-specific execution.
- `stream/agentic_test.go` — **new** under existing `stream/`: real AgenticModel fixtures, all variants and failure/resource cases.
- `internal/testmodel/agentic.go` and `internal/testmodel/agentic_test.go` — **new** under existing `internal/testmodel/`: deterministic `model.AgenticModel` and stream-reader fixtures.
- `stream/doc.go` — existing package documentation updated with classic/agentic entry points and host ownership.

Proposed API:

```go
type BlockContextResolver interface {
    ResolveBlock(index int, kind schema.ContentBlockType) (convert.AgenticBlockContext, error)
}

type TransientSink interface {
    Emit(events.Event) error
    Detach(error)
}

type AgenticStreamIdentity struct {
    SessionID string
    RunID string
    TurnID string
    MessageID string
    AttemptID string
    AgentPath []convert.AgentPathSegment
}

func StreamAgenticTurn(
    ctx context.Context,
    am model.AgenticModel,
    messages []*schema.AgenticMessage,
    ids AgenticStreamIdentity,
    blocks BlockContextResolver,
    opts ...AgenticOption,
) (*AgenticResult, error)
```

`AgenticOption` includes pass-through model call options as a copied slice, optional `WithTransientSink`, and the shared positive `ProjectionLimits`. It never includes tool executors, commit receipts or persistence callbacks. `AgenticResult` contains the concatenated `Assistant`, final candidate `PublicProjection` when validation succeeds, successfully delivered transient facts, detachable `ObserverErr`, token usage, seen block indices, `Partial`, and terminal classification. The host commits the candidate and separately calls W2's committed-emission API.

## Stream algorithm and invariants

1. Validate the base identity, shared limits and resolver before opening the model stream. Clone the input slice/pointers or document them as read-only; never write Eino streaming metadata into caller messages.
2. Call `AgenticModel.Stream` with the caller's context and explicit Eino options. An open failure returns nil result and no event.
3. Receive until actual EOF. Honor `WithOnEOF` final values/errors exactly once. Reject nil chunks, invalid roles, mixed final message roles, negative/duplicate-conflicting indices and malformed chunk unions.
4. Correlate by non-negative `StreamingMeta.Index`. Resolve complete block context once at first observation and require the same kind/context for that index. Server-tool provider identity and expected approval correlation therefore come from the caller, never Eino fields. Unindexed single-shot blocks may be assigned final ordinal only when their order is unambiguous; mixed indexed/unindexed chunks are an error.
5. Emit only self-contained native chunk events for safely incremental reasoning, assistant text and function calls, each with closed identity metadata marked `transient: true`. Do not open native start/end lifecycles during pre-commit streaming. Buffer custom-rich blocks, structured results and all authoritative content until the host commits the returned final projection. Do not emit server/MCP/tool-search execution claims from raw provider chunks.
6. Because transient chunk events are self-contained, block transitions and failed exits require no synthetic completion. If the optional sink errors, call `Detach` once, store `ObserverErr`, disable only that sink and continue draining the model. After commit, W2 emits the selected live-continuation supplement or replay/committed-only native sequence exactly once.
7. Concatenate with real `schema.ConcatAgenticMessages` at finalization. Project the result with W1, applying shared limits before full cloning/encoding, and compare its block index/kind/context ledger to the live ledger.
8. On execution-context cancellation, receive error, correlation error, limit error or concat error after opening, return a non-nil partial result and the primary error. Observer transport failure is nonfatal and never cancels execution. Never emit turn/run completion.
9. Defer reader `Close` immediately after a successful open. Preserve the primary receive, correlation, limit or execution-context error; expose close error only when there is no earlier error, or join it without changing the terminal classification if the API supports deterministic inspection. A detached observer error remains separately inspectable and is never promoted into the execution terminal classification.

Transient model chunks do not become replay state. `AgenticResult.PublicProjection` is an uncommitted candidate. No final custom block, tool result, native block end, pause or completion fact is emitted until the host returns a matching `CommitReceiptV1` to W2.

## Attempts, tools and privacy

- The host supplies a distinct `AttemptID` per physical model request. The stream helper never retries.
- An attempt that fails after output remains partial. The caller emits `AttemptReplaced` before starting a successor attempt and chooses whether prior live output is visually replaced.
- Function-tool call argument fragments are correlated and may be emitted as transient `TOOL_CALL_CHUNK` events but are never executed. Completed function calls wait for host commit. Server-tool and MCP blocks are explicitly provider-owned records and never enter `tools.ClassifyToolCalls`.
- Native MCP approval blocks are content. They do not cause pause or interrupt emission.
- Projection strips private fields before any custom event. Chunk buffering must not log or interpolate payload bodies into errors.

## Verification and acceptance

- Use a real `model.AgenticModel` implementation in tests, not classic adapters or hand-calls into state methods.
- Cover all 20 block kinds in generation and streaming, including interleaved indices, split arguments/content, mixed ordered blocks and all five function-result parts.
- Exercise `schema.ConcatAgenticMessages` and a `StreamReader` with `schema.WithOnEOF` final value, final error and ordinary EOF.
- Test open error, nil chunk, empty stream, mid-stream error, malformed union, index/kind conflict, byte/block limit, explicit execution-context cancellation and observer transport failure.
- Count `Close` calls and goroutines for success and every execution error path. Use repeated race tests for cancellation versus EOF and sink detachment versus continued execution.
- Prove observer write/flush failure and observer-context cancellation detach the sink while the model reaches EOF and produces a candidate. Prove explicit execution cancellation still closes the reader and returns partial state.
- Assert partial attempts may contain only identified transient native prefixes and never emit authoritative final/custom/tool-result/pause/`turn_finished`/`RUN_FINISHED` facts or private sentinels.
- Simulate host commit failure after a successful drain and assert no committed/final bytes; then emit with a matching receipt and compare against replay.
- Assert input messages, block structures and model-option slices are unchanged after success and failure.
- Run `go test ./stream ./internal/testmodel` and `go test -race -count=10 ./stream` for focused cancellation/correlation cases.

Exclusions: retries, failover choice, tool invocation, checkpointing and durable commit order remain host responsibilities.
