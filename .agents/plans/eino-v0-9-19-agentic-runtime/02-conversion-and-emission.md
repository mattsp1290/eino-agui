# W2 — Agentic conversion and AG-UI emission

Goal: translate validated public agentic projections into standard AG-UI events plus the single namespaced custom envelope. Prerequisite: W1 contract and privacy tests pass.

## Existing patterns

- `convert.ToAGUIMessages` builds classic snapshots but generates message IDs and cannot preserve mixed agentic block order.
- `emitter.Emitter.Emit` is the single validation/encoding/transport path. Encoding errors are recoverable; transport errors cancel once and stop later writes.
- Typed emitter helpers track open text, reasoning and function-tool blocks and close them before incompatible event families.
- The SDK's `EventDecoder` and strict JSON decoder are available for round-trip acceptance.

## Proposed change surface

- `convert/agentic.go` — **new** under existing `convert/`: `ToAgenticProjection`, native snapshot conversion and public annotation projection.
- `convert/agentic_test.go` — **new** under existing `convert/`: per-role/native/custom mapping tests.
- `emitter/agentic.go` — **new** under existing `emitter/`: agentic block, turn, attempt, pause and cancellation helpers that all call existing `Emit`.
- `emitter/agentic_test.go` — **new** under existing `emitter/`: ordering, strict decode, transport and privacy cases.
- `emitter/emitter.go` — existing insertion point for generalized close/flush bookkeeping shared by classic and agentic blocks.
- `convert/doc.go` and `emitter/doc.go` — existing package documentation updated with agentic contracts and ownership limits.
- `docs/architecture/package-origins.md` — existing support matrix updated to distinguish classic and agentic paths.
- `docs/decisions/0001-eino-version-floor.md` and `docs/decisions/0002-ensemble-shared-surface.md` — existing decisions amended or superseded by 0005 so they no longer state that agentic/ADK support is excluded.

Proposed entry points:

```go
func ToAgenticProjection(msg *schema.AgenticMessage, ctx AgenticProjectionContext) (*AgenticProjection, error)
func NewObserverEmitter(ctx context.Context, w *bufio.Writer, sw *sse.SSEWriter) *Emitter
func (e *Emitter) EmitTransientBlock(block convert.TransientBlock) bool
func (e *Emitter) EmitCommittedProjection(projection *convert.AgenticProjection, receipt convert.CommitReceiptV1, mode DeliveryMode) bool
func (e *Emitter) TurnStarted(input convert.TurnStartedV1, receipt convert.CommitReceiptV1) bool
func (e *Emitter) TurnFinished(input convert.TurnFinishedV1, receipt convert.CommitReceiptV1) bool
func (e *Emitter) AttemptReplaced(input convert.AttemptReplacedV1, receipt convert.CommitReceiptV1) bool
func (e *Emitter) Paused(input convert.PausedV1, receipt convert.CommitReceiptV1) bool
func (e *Emitter) Resumed(input convert.ResumedV1, receipt convert.CommitReceiptV1) bool
func (e *Emitter) Cancelled(input convert.CancelledV1, receipt convert.CommitReceiptV1) bool
```

`AgenticProjection` is an ordered list of projected blocks, not one flattened `types.Message`. Each projected block declares its native events and mandatory committed `eino.agentic.v1` identity/ordinal supplement. Construct and validate the entire final/replay projection before emitting its first event. Live streaming may already have emitted valid transient deltas before a later malformed chunk; that path returns an explicit partial transcript and never emits an authoritative committed supplement or completion fact. The projection includes its public candidate digest, so hosts do not reproduce private serialization to build receipts.

## Emission behavior

- Define `DeliveryModeLiveContinuation`, `DeliveryModeCommittedOnly` and `DeliveryModeReplay`. `LiveContinuation` follows already-sent chunks with only the custom commit/supplement fact and never resends native content. `CommittedOnly` and `Replay` emit the complete canonical native sequence once because no transient prefix exists. All modes require a matching receipt and emit in block order.
- Use the SDK's self-contained `TEXT_MESSAGE_CHUNK`, `REASONING_MESSAGE_CHUNK` and `TOOL_CALL_CHUNK` families for pre-commit live deltas; do not emit native start/end/result completion sequences before commit. Use caller message IDs for native text/reasoning ownership. Put the closed identity with `transient: true` at `metadata["eino.agentic.v1"]` on every live chunk and the same identity without that marker on post-commit fragments. Transient SSE event IDs/timestamps may remain SDK-generated.
- Attach agent/subagent identity directly to SDK fields where available and place the full ordered agent path in the custom identity. Do not derive durable subagent run IDs from agent names.
- Keep native function call parent IDs equal to the owning caller message ID. Preserve call ID and name; reject missing IDs instead of synthesizing them.
- Emit text-only function results natively only after commit. The authoritative structured custom representation is mandatory when more than one nested part or any media part exists.
- `TurnStarted`, `TurnFinished`, `Paused`, `Resumed`, `Cancelled` and `AttemptReplaced` are committed custom facts and require a matching host receipt. `Resumed` links the prior pause, the exact full/partial ADK targets, any separately validated MCP approval pair, and the new turn/attempt IDs.
- Add committed run-ending wrappers around existing `RunFinishedSuccess`, `RunFinishedInterrupt` and `RunError`; require a receipt and explicit `loopSettled=true`. No message/turn helper emits an enclosing run terminal event. `RunStarted` requires the host's admitted-run receipt.
- `Paused` contains public interrupt IDs/addresses supplied by the host and optional validated native MCP approval correlation. It excludes checkpoint bytes and arbitrary `InterruptInfo.Data`.
- Pre-encode and validate every event in a buffered final/replay logical block before its first write. Encoding failure records `EncErr` and writes none of that buffered block. Transport failure can still leave a partial wire prefix; keep existing cancel-once behavior, record the successfully written facts, and write nothing later.
- Treat a live transport failure as observer detachment. Agentic delivery must construct or wrap `Emitter` without an execution-cancel callback; it disables that sink and does not stop the model/ADK source or create a durable cancellation candidate. Preserve the existing cancel-on-write behavior for the classic request-bound API.
- `NewObserverEmitter` has no thread/run ownership and no cancellation callback. Its adapter reports transport failure to the drain as a detachable observer error. Never pass an execution cancel function to this constructor.

## Compatibility and lifecycle

No backward compatibility is required and no feature flag applies. Keep classic helpers source-stable when practical, but do not contort agentic types into classic message fields. Existing golden fixtures remain as classic regression evidence; add separate agentic fixtures instead of rewriting their meaning.

If an emitter becomes unusable after transport failure, W3/W4 return a partial result containing only successfully written public facts. The host remains authoritative for committed state and can reconstruct replay through fresh conversion.

## Verification and acceptance

- For every W1 mapping, encode through `sse.SSEWriter`, parse SSE frames, decode with `events.NewEventDecoder(nil)` and strict JSON decoding, then decode `eino.agentic.v1` values with `DecodeAgenticEnvelope`.
- Assert exact event order for mixed reasoning → text → function call → generated media → result sequences.
- Assert native/custom merge produces the same ordered public blocks as direct projection with no duplication.
- Feed live chunks then a committed `LiveContinuation` fact through the exact TypeScript client chunk/apply path at the pinned AG-UI commit and prove text, reasoning and tool arguments appear once with no overlapping lifecycle. Feed `Replay` into a fresh reducer and prove the same final view. Resolve that checkout through `AG_UI_REPO_DIR`; do not add a production TypeScript dependency.
- Drop the live sink after a partial prefix, allow execution/commit to continue, reconnect with a fresh reducer and replay the committed projection once without duplication.
- Verify observer context cancellation and SSE write/flush failure never call the model/ADK abort hook and never create a `cancelled` candidate. Verify explicit execution cancellation still aborts/joins once through W4.
- Inject an SDK validation error and a writer/flush error at each buffered multi-event block boundary. Encoding failure emits no prefix for that buffered logical block; agentic observer transport failure explicitly returns a partial prefix and detaches the sink once without invoking execution cancellation. Retain the existing cancel-once assertion only for the classic request-bound emitter.
- Feed a late malformed live chunk after valid native deltas and assert an explicit partial transcript with no committed supplement or completion fact.
- Verify `TurnFinished` never produces `RUN_FINISHED`, and multiple normal turns under one run produce one run start and no terminal event until an explicit host terminal call.
- Verify commit failure or a missing/mismatched receipt emits no final block, tool result, pause/resume, subagent completion, turn or run terminal bytes.
- Verify public pause/resume payloads keep MCP approval ID and ADK address distinct and partial multi-target resume is replayable.
- Run `go test ./convert ./emitter` and `go test -race ./convert ./emitter`.

Exclusions: no automatic replay store, no lifecycle inference from arbitrary event order and no additional custom event namespace.
