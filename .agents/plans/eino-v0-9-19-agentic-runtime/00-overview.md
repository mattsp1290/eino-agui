# Eino v0.9.19 agentic runtime bridge

Status: Ready for implementation. Planning only; no implementation has occurred. Planning bead: `eino-agui-mnl`.

## Application context

```json
{
  "application_context": {
    "has_active_users": false,
    "backward_compatibility_required": false,
    "feature_flags": "not-applicable",
    "confirmation_digest": "b51552ee583bacc54afe25831e38b17a84bc3f06822ed85bea562377479a436c",
    "confirmed_at": "2026-09-10T04:10:35Z"
  }
}
```

The user answered “1. no 2. no.” The implementation can add breaking agentic APIs and replace the earlier exclusion without compatibility shims or feature flags. Existing classic APIs remain only where they still provide a coherent, tested classic bridge; their retention is an architectural choice, not a compatibility promise.

## Outcome and success criteria

Extend the reusable `convert`, `emitter`, and `stream` boundaries to carry sanitized Eino v0.9.19 `schema.AgenticMessage` content and typed ADK lifecycle observations into AG-UI. Preserve ordered block and lifecycle identity without making this library a session store, execution engine, approval authority, or checkpoint owner.

Implementation succeeds when:

- every one of Eino v0.9.19's 20 `schema.ContentBlockType` variants has an explicit, tested public projection with no silent flattening or omission;
- malformed or ambiguous final/replay unions fail before that projection emits any event; a malformed late live chunk returns only the already-written transient prefix and no authoritative completion;
- standard AG-UI events carry representable text, reasoning, function-tool, run, and subagent behavior while one proposed namespaced schema, `eino.agentic.v1`, supplies native-event identity metadata and the custom envelope for semantics the pinned SDK cannot represent;
- caller-supplied session, run, turn, message, block, agent-path, call, approval, interrupt, and attempt identities survive conversion, streaming, replay, and retry replacement without bridge-generated durable identity;
- typed ADK event streams distinguish message output, business interrupt, cancellation, error, child-agent attribution, normal turn completion, and enclosing run termination;
- native MCP approval request IDs and ADK interrupt addresses remain separate and are correlated only from caller-validated input;
- signatures, encrypted reasoning, provider continuation IDs or blobs, arbitrary `Extra`, arbitrary extension values, Gemini SDK blobs, and private ADK checkpoint state never enter emitted bytes;
- real Eino stream helpers and the actual AG-UI SSE writer and strict decoder pass success, EOF, error, cancellation, reader-close, partial-attempt, input-nonmutation, and private-sentinel cases;
- an `eino-agent` W2/W5 fixture compares persisted-reopen/replay and live-final public projections without introducing a production dependency on `eino-agent`; and
- an immutable `eino-agui` pin is downloadable and a fresh `GOWORK=off` consumer succeeds with no local path. If the AG-UI fork replacement is still necessary, the response and README name the exact remote replacement and the clean-consumer fixture includes it explicitly.

## Change type and affected areas

This is a breaking library/API and protocol-projection change. It affects `convert`, `emitter`, `stream`, internal test fixtures, golden/parity coverage, dependency verification, CI, examples, README and architecture/decision documentation. It does not add persistence, HTTP routes, authorization, credentials, tool execution, or a UI.

## Repository findings

- `go.mod` already pins `github.com/cloudwego/eino v0.9.19` and resolves the AG-UI SDK through the immutable fork pseudo-version `v0.0.0-20260909025854-aaa75b54d572`.
- Go replacements are not transitive. The current README already requires each consumer to repeat that remote replacement.
- `convert/convert.go` converts only classic `schema.Message`. It preserves a small package-owned metadata envelope, but it currently exposes encrypted tool-call continuity on conversion and drops unsupported multimodal content.
- `emitter/emitter.go` centralizes validation, SSE encoding, transport cancellation and open-block closure. It already supports generic `Emit`, run outcomes, subagent events and encrypted-value scrubbing for snapshots.
- `stream/stream.go` and `stream/state.go` own one classic `ToolCallingChatModel` turn. They concatenate classic chunks, correlate function calls, return partial state after stream-open errors, and always close the reader.
- Eino v0.9.19 defines exactly 20 `ContentBlockType` values, five nested function-result media variants, `model.AgenticModel`, `schema.ConcatAgenticMessages`, and typed ADK events whose message output can be complete or streaming.
- Upstream documents that `TypedAgent[*schema.AgenticMessage]` does not yet wire model-stream cancellation monitoring or retry. This bridge must project caller-observed outcomes and must not claim to supply missing runtime control.
- The pinned AG-UI SDK represents user multimodal input, assistant text, reasoning, function tools, interrupts, subagents, run outcomes and generic custom events. It has no native event family for generated media, provider server tools, MCP/list/approval records, tool-search definitions, durable turns, or attempt replacement.
- The clean baseline at `6d71f8e` passes `go test ./...`. The worktree had no changes before planning.
- The demonstrated classic consumer is `github.com/mattsp1290/eino-agent`. Its proposed W2/W5 contracts define durable public content, one run across multiple turns, committed-before-emission final state, explicit agent/attempt identity, and host-owned checkpoint and approval semantics.

## Decisions

1. Add parallel agentic entry points instead of widening classic functions with `any` or mode switches. Compile-time `schema.AgenticMessage` and `model.AgenticModel` types keep the two stream grammars unambiguous.
2. Define proposed public structs in `convert/agentic_types.go` and a strict versioned codec in `convert/agentic_codec.go`. Do not transport raw Eino structs, `Extra`, or provider extension objects.
3. Use the namespace `eino.agentic.v1` for one closed schema. `CUSTOM` events with that name carry rich content and lifecycle facts; native-event `metadata["eino.agentic.v1"]` carries the same closed identity type. Native events remain the user-facing projection where the SDK is sufficient.
4. Treat caller-supplied identity as authoritative. The bridge validates and correlates identity but generates only transient AG-UI frame IDs where the SDK requires them.
5. Keep execution and durability host-owned. The bridge drains supplied model or ADK streams, emits observations, and returns a final/partial projection; it never invokes tools, stores checkpoints, creates sessions, or converts MCP approval content into an ADK interrupt.
6. Retain classic APIs during this change because they serve a distinct classic model boundary. Mark the new agentic API as the rich path and update the old decisions that currently describe agentic support as excluded.
7. Decouple the host execution drain from every observer transport. A failed or cancelled SSE/watch sink detaches without cancelling the model/ADK producer; only an explicit host execution cancellation reaches the source abort hook or creates a cancellation candidate.

Rejected alternatives:

- Raw `Extra` passthrough can leak private provider and ADK state and has no stable decoding contract.
- Encoding all rich content as text loses type, order, identity and replay equivalence.
- Emitting every block only as `CUSTOM` discards standard AG-UI behavior for clients that already understand native text, reasoning and function-tool events.
- Making the bridge own TurnLoop, checkpoint or approval orchestration duplicates `eino-agent` authority and violates the request boundary.
- Treating MCP approval IDs as ADK interrupt IDs creates unsafe implicit authorization and incorrect resume targets.

## Target flow

```text
host-owned committed identity + sanitized Eino/ADK input
    -> strict agentic union validation and public/private projection
    -> stream correlation by Eino block index plus caller block context
    -> optional observer sinks receive transient native chunks tagged with identity
       (sink failure detaches; execution continues)
    -> validated final candidate returned to the host
    -> host commit plus matching projection digest/revision receipt
    -> committed native/custom final facts through eino.agentic.v1
    -> SDK SSE writer
    -> strict SDK decoder + eino.agentic.v1 decoder
    -> live public projection comparable with durable replay
```

Completed model chunks are transient. W3/W4 return final candidate projections and observations without emitting authoritative final blocks, tool results, pause/resume, cancellation, subagent completion or terminal facts. The host commits public state, then passes a receipt whose projection digest and revision match the candidate to the committed-emission helpers. Commit failure therefore produces no final semantic notification. `TURN_FINISHED` in the custom envelope does not imply `RUN_FINISHED`; the latter is emitted only when the host reports that the enclosing loop settled.

## Scope and constraints

In scope: agentic conversion, public projection validation, model and ADK stream draining, lifecycle event emission, stable identity correlation, privacy filtering, tests, documentation, example, publication proof and the requested owner response.

Out of scope: session persistence, replay storage, database schemas, tool execution, checkpoint codecs, approval policy, interrupt authorization, route/tenancy configuration, credentials, asset fetching, UI rendering, provider implementation, and changes inside `eino-agent` beyond a coordinated test fixture.

The implementation must use Eino v0.9.19 APIs and the pinned AG-UI encoder/decoder. It must not depend on a workspace or filesystem replacement for publication evidence. The bridge may require the documented immutable remote AG-UI replacement until the forked SDK is published under a transitive canonical dependency path.

## External request map

Resolve the consumer checkout through `EINO_AGENT_DIR`, pointing to `github.com/mattsp1290/eino-agent`. Resolve Eino with `go mod download -json github.com/cloudwego/eino@v0.9.19`. Resolve request documents below `$HOME/.agents/projects`.

| Canonical location | Owner and consumers | Affected work | Status and unblock evidence |
| --- | --- | --- | --- |
| `$HOME/.agents/projects/eino-agui/requests/2026-09-10-eino-v0-9-19-agentic-runtime.md` | Matt / eino-agui maintainers; demonstrated classic `eino-agent`, proposed agentic `eino-agent` | All packages; especially W4–W5 | Accepted for planning by this specification. Consumer W7/final W8 remain blocked until implementation tests and an immutable pin are recorded in the same-filename response. |
| `$HOME/.agents/projects/eino-agui/responses/2026-09-10-eino-v0-9-19-agentic-runtime.md` (proposed planning response) | eino-agui maintainers; consumed by `eino-agent` | W5 publication | Must distinguish plan acceptance from implementation completion. It unblocks the consumer only after it records exact pins, commands/results, constructor signatures, mapping contract and clean-consumer evidence. |

No new repository is proposed. No outbound dependency request is needed: Eino already owns the agentic and ADK APIs, while the current AG-UI fork constraint is an explicit downstream publication condition rather than an unaccepted new capability request.

## Risks, assumptions and gates

- Stop/go gate: fail W1 if a complete, bounded public projection cannot exclude private extension data without losing a requested public field. Revise the allowlist and tests before implementing stream emission.
- Stop/go gate: fail W3 if block indices cannot be correlated deterministically or an error path can emit `TURN_FINISHED`/`RUN_FINISHED` after partial output.
- Stop/go gate: fail W4 if a typed ADK source cannot provide context-aware receive plus idempotent abort-and-wait semantics that unblock an in-flight receive. Do not ship a raw blocking-iterator API that can leak its producer.
- Publication gate: do not publish or mark the response complete until a clean `GOWORK=off` temporary consumer downloads the exact bridge pin and compiles/tests without local source access.
- Dependency gate: if the root AG-UI replacement remains necessary, state that exact fact and pin. Do not call the dependency graph self-contained or hide the replace through `go.work`.
- Delivery gate: fail W2/W3 if the exact pinned AG-UI client reducer duplicates live content when a post-commit supplement arrives or if replay into a fresh reducer differs from the committed live view.
- Non-blocking assumption: the host can supply stable block IDs and per-block provider/approval context as soon as Eino exposes a streaming block index. Missing or duplicate durable identity or required context is a deterministic input error.
- Non-blocking assumption: inline media is already sanitized by the host. The bridge independently applies shared limits before cloning or encoding and does not fetch URLs.

There are no unresolved blocking user decisions. Feature flags are not applicable.

## Document map

- [01-public-contract.md](01-public-contract.md): strict identity, privacy, codec and 20-kind mapping contract.
- [02-conversion-and-emission.md](02-conversion-and-emission.md): conversion APIs, native/custom emission and documentation work.
- [03-agentic-streaming.md](03-agentic-streaming.md): AgenticModel chunk correlation, partial results and resource lifecycle.
- [04-adk-lifecycle.md](04-adk-lifecycle.md): typed ADK event draining, turn/run semantics, pause, retry and cancellation projection.
- [05-verification-and-publication.md](05-verification-and-publication.md): real-codec tests, consumer fixture, dependency proof and response contract.
- [06-execution-handoff.md](06-execution-handoff.md): dependency-ordered packages, commands, gates and definition of done.
