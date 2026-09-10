# Execution handoff

Status: Ready for implementation. Application context is complete. No implementation has occurred.

## Dependency-ordered work packages

| Order | Package | Prerequisites | Bounded result | Verification |
| --- | --- | --- | --- | --- |
| 1 | W1 public contract and privacy | Exact dependency pins | Closed bounded public types, strict codec, caller block context, 20-kind/tool-definition map and provider allowlist | `go test ./convert`; `go test -race ./convert` |
| 2 | W2 conversion and emission | W1 | Identified transient native deltas plus receipt-gated native/custom final facts in one `eino.agentic.v1` schema | `go test ./convert ./emitter`; race variants |
| 3 | W3 AgenticModel stream | W1–W2 | Correlated transient chunks, detachable observer, final candidate, usage and exact reader cleanup | `go test ./stream ./internal/testmodel`; focused race `-count=10` |
| 4 | W4 typed ADK lifecycle | W1–W3 | Transport-independent source drain, explicit abort/join, child identity, turn/run separation and pause/resume/cancel/attempt candidates | `go test ./stream ./emitter ./internal/testmodel`; focused race cases |
| 5 | W5 integration/publication | W1–W4 and consumer fixture coordination | Golden/inventory/CI gates, docs/example, clean consumer proof, immutable pin and completed response | full commands in W5 plus consumer W2/W5 fixture |

W1 must land before implementation spreads public types across packages. W2 and deterministic test-fixture preparation can proceed in parallel after the W1 codec is fixed. W3 and W4 both touch stream lifecycle and should be sequential unless file ownership is explicitly split. W5 documentation can begin early, but publication evidence and response completion wait for all preceding gates.

No feature-flag decision gates apply. Breaking public APIs are permitted by the confirmed application context. Keep unrelated worktree changes unstaged if the baseline changes before execution.

## Package details and insertion points

- W1 adds **new** files under existing `convert/` and `docs/decisions/`. It may extend existing `internal/protocolmeta/protocolmeta.go` only for public-value cloning.
- W2 adds **new** `convert/agentic.go`, `emitter/agentic.go` and tests under existing package directories. It modifies existing emitter block bookkeeping and package docs.
- W3 adds **new** `stream/agentic*.go` and `internal/testmodel/agentic.go` under existing parents. It sends transient events through the W2 detachable observer adapter while retaining the existing SDK SSE writer/decoder path.
- W4 adds **new** `stream/adk.go`, `emitter/adk.go`, tests and `examples/agentic/main.go` under existing parents. It imports public Eino ADK types but no host runtime.
- W5 adds **new** golden files, `testdata/consumer/`, `docs/architecture/agentic-contract.md`, and the external response. It modifies existing CI, parity, README, decisions and dependency inventory tests.

Every new symbol named in the work files is proposed. The implementer may rename a proposed symbol only when Go type constraints or verified SDK conventions require it; update all documentation and consumer fixtures in the same change.

## Integration and regression gates

1. Classic `convert`, `emitter`, `stream` and `tools` tests and golden fixtures remain green.
2. Exact inventory covers 20 outer block kinds, five nested function-result kinds and all `ToolInfo` parameter representations.
3. All native and custom frames pass actual SDK encode/decode and the custom strict decoder.
4. Private sentinel scans cover successful, malformed, partial, error, pause, cancellation and replay paths.
5. Commit failure emits no authoritative fact; two-turn post-commit live and SQL-reopen projections match on semantic IDs/order with pause/resume and one enclosing run terminal fact.
6. `go build ./...`, `make check`, full tests, race tests and parity tests pass.
7. A clean `GOWORK=off` consumer downloads the exact candidate pin without local source paths. Any required root AG-UI replacement is explicit and remotely pinned.
8. Sink detach does not cancel execution; the exact pinned AG-UI TypeScript reducer receives no duplicate live content and fresh replay produces the same final view.

Stop implementation and revise the plan if:

- public/private projection cannot be made closed and lossless for requested public semantics;
- a final/replay malformed message can emit a prefix, or a late live error can emit anything beyond an explicitly partial transient prefix;
- streamed block identity cannot be correlated without the bridge inventing durable identity;
- typed ADK output cleanup requires consuming an exclusive stream twice or the supplied source cannot unblock and join an in-flight receive;
- a normal turn or pause necessarily emits an enclosing run terminal event; or
- an observer sink is coupled to execution abort, or live-continuation/replay duplicates content in the pinned AG-UI reducer; or
- publication proof needs a workspace or filesystem replacement.

## Definition of done

- The public API accepts bounded sanitized Eino v0.9.19 agentic messages and context-aware typed ADK event sources.
- All requested content/tool-definition shapes, identity, lifecycle, post-commit pause/resume correlation, retry replacement and cancellation projections satisfy the measurable tests.
- Native server/MCP records remain distinct from host-local function tools.
- The bridge owns no persistence, tool execution, checkpoint, authorization, route or tenancy behavior.
- Private provider and ADK state is absent from all public bytes and error strings.
- Documentation includes exact constructors, mappings, ownership, dependency instructions and the custom decode contract.
- Candidate digests and lifecycle digests have public constructors, fixed vectors and domain-separated identity/kind binding.
- The inbound response records exact implementation, publication and consumer evidence and is accepted by `eino-agent` before its blocked W7/W8 are called complete.
- Implementation changes are reviewed, committed and pushed according to repository workflow; the planning bead is not reused as the implementation issue.

## Deferred and follow-up work

- Canonical publication of the AG-UI fork is a separate upstream concern unless implementation chooses to remove fork-only APIs. Until then, downstream roots carry the exact remote replacement.
- UI rendering of rich/generated media, citations, grounding and approval controls stays with consumers.
- Provider-native model adapters stay with their provider owner.
- Any future Eino content kind or public provider extension requires a versioned contract revision and inventory update.

## First implementation action

Open [01-public-contract.md](01-public-contract.md), create a dedicated implementation bead, and implement only the strict public types/codec plus exhaustive union/privacy tests before adding emission or streaming.
