# W5 — Integration verification, dependency publication and response

Goal: prove the bridge through real Eino and AG-UI APIs, a coordinated durable consumer fixture and a downloadable module pin. Prerequisites: W1–W4.

## Proposed change surface

- `testdata/golden/agentic_convert.normalized.json` — **new** under existing `testdata/golden/`: all public content and annotation shapes.
- `testdata/golden/agentic_stream.normalized.json` — **new** under existing `testdata/golden/`: mixed live stream and lifecycle frames.
- `testdata/golden/README.md` — existing fixture contract updated with native/custom merge rules and private-field exclusions.
- `internal/golden/feature_inventory_test.go` — existing insertion point for exact 20-kind and five nested-result inventory gates.
- `internal/deps/version_test.go` — existing insertion point for Eino AgenticModel/ADK and AG-UI decoder compile-time contracts.
- `parity_test.go` — existing parity runner updated to include agentic packages/fixtures without redefining classic reference parity.
- `testdata/consumer/` — **new** under existing `testdata/`: standalone temporary-module fixture and `check.sh` for `GOWORK=off` resolution and execution.
- `testdata/agui-client/` — **new** under existing `testdata/`: live-continuation/replay conformance against the exact TypeScript client chunk/apply reducer resolved through `AG_UI_REPO_DIR` at commit `aaa75b54d572be8cd1d51c72e951273c5b893ed0`.
- `.github/workflows/ci.yml` and `.github/workflows/parity.yml` — existing jobs updated for agentic race/inventory/consumer checks.
- `README.md` — existing install, API, ownership, agentic mapping and AG-UI replacement instructions updated.
- `docs/architecture/agentic-contract.md` — **new** under existing `docs/architecture/`: constructor signatures, full mapping, identity merge and host responsibilities.
- `examples/agentic/main.go` — **new** credential-free executable under existing `examples/`.
- `$HOME/.agents/projects/eino-agui/responses/2026-09-10-eino-v0-9-19-agentic-runtime.md` — **proposed external planning response** at the request's required response location; final implementation edits record exact pins/results.

## Consumer coordination

Resolve `EINO_AGENT_DIR` to a checkout of `github.com/mattsp1290/eino-agent`. The coordinated fixture may copy or generate stable W2/W5 public contract data into this repository's `testdata/consumer/`, or run a test-only command in the consumer checkout. It must not add a production import from `eino-agui` back to `eino-agent`.

Use the consumer's durable APIs to create a fixture containing all 20 public block kinds, two normal turns under one run, child-agent identity, partial attempt replacement, native server versus local function calls, MCP request/response correlation, pause/resume and cancellation. Capture two projections:

1. close/reopen SQL and project public replay through the bridge; and
2. project the same committed final records through the post-commit bridge API, using a matching revision/digest receipt after transient live deltas.

Normalize only transport-generated event IDs/timestamps. Assert semantic identities, order, public payloads and terminal facts are equal. Inject commit failure between drain and receipt and assert no final/custom/tool-result/pause/resume/terminal notification. Scan SQL-facing public JSON, emitted SSE and decoded objects for private sentinels. Provider state/checkpoints stay in the consumer fixture and are never passed into the bridge.

Run the live stream through the exact AG-UI TypeScript chunk/apply reducer before custom decoding. Assert live chunks plus a `LiveContinuation` commit supplement render each text/reasoning/tool-argument byte once. Assert a fresh reducer fed `Replay` reaches the same final view. Disconnect the first sink before commit, continue the host execution, then replay after reconnect and prove the execution was not cancelled and the fresh view is complete.

If coordination cannot run in CI because the consumer API is not yet implemented, land a contract fixture and command with a failing/blocked execution bead rather than weakening the local unit tests. Do not mark the inbound request complete until the real W2/W5 fixture passes.

## Dependency and publication proof

Before publication:

1. Run `go mod download -json github.com/cloudwego/eino@v0.9.19` and verify tag origin SHA `9d983b36a5112a1c233056b1a099825298fafb8f` through the existing dependency test pattern.
2. Verify the AG-UI fork pseudo-version downloads at full commit `aaa75b54d572be8cd1d51c72e951273c5b893ed0`.
3. Create a fresh temporary module outside any workspace. Set `GOWORK=off`, require the candidate immutable `github.com/mattsp1290/eino-agui` pin and make local checkout paths inaccessible.
4. If the canonical AG-UI dependency is still not reachable, add the exact remote replacement to the temporary root and assert the fixture reports that requirement. Never use a filesystem replacement.
5. Compile and run the actual agentic conversion, emitter, stream and typed ADK fixture through the downloaded module.
6. Record `go list -m -json all`, the bridge version/commit and checksums without credentials or local paths. Do not call the graph self-contained while a root replacement remains required.

Publication itself is an implementation/release action, not part of this planning session. The implementer must obtain an immutable tag or commit available through the configured remote before completing the response.

## Response contract

The same-filename response must keep separate statuses for:

- owner acceptance of the plan/contract;
- implementation completion;
- immutable bridge publication;
- clean consumer verification; and
- `eino-agent` verification/acceptance.

It must list exact exported constructor, limits, projection-context, event-source and type signatures; the 20-kind mapping; `eino.agentic.v1` schema/version; privacy exclusions; host ownership; all test commands/results; bridge and AG-UI pins; whether a root replacement is required; and remaining gaps. A local commit, request creation, plan completion or workspace test does not unblock `eino-agent` W7/W8.

## Quality gates

Run after implementation:

```bash
go build ./...
make check
go test ./...
go test -race ./convert ./emitter ./stream ./tools
go test ./... -run Parity -count=1
AG_UI_REPO_DIR=<exact-fork-checkout> testdata/agui-client/check.sh
GOWORK=off testdata/consumer/check.sh <immutable-bridge-pin>
```

Run the coordinated consumer command documented by `EINO_AGENT_DIR` after its W2/W5 fixture exists. The final response records exact commands and results rather than a generic “tests pass.”

## Acceptance

- Inventory tests fail if Eino adds/removes a content kind, nested result variant or tool-parameter representation without an explicit mapping update.
- Golden tests use real SDK encoding and strict decoding and preserve all semantic IDs for simple native-only-shaped and mixed rich blocks.
- Fixed digest vectors match an independent consumer canonicalizer and reject altered identity, kind and payload.
- The exact pinned TypeScript reducer sees no duplicate live content and fresh replay reaches the same committed view.
- Race tests cover cancellation/EOF, source abort/join, nested-reader cleanup and emitter transport failure.
- The example demonstrates two committed turns, one run, one child, one rich custom block, pause/resume and one explicit settled terminal event without credentials or persistence ownership.
- The downloadable consumer proof has no local path or workspace dependency.
- The response stays “implementation incomplete” until all required implementation and consumer evidence exists.

Exclusions: sending an external message, publishing upstream AG-UI changes, changing `eino-agent` production code or claiming consumer acceptance on its behalf.
