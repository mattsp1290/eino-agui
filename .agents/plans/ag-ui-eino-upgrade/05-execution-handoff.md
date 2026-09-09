# 05. Execution Handoff

## Operating context

No active users or external consumers were reported. Backward compatibility is not required. Feature flags are not applicable. Implement the clean target API and do not add deprecated adapters unless new evidence changes that context.

Track implementation in Beads. Run `bd prime`, create or claim the implementation issue before editing code, and follow the repository's mandatory commit/push completion protocol.

## Dependency-ordered work packages

### WP1 — Resolve exact dependency pins

Changes:

- `go.mod`, `go.sum`;
- `internal/deps/version_test.go` (`new`);
- Eino and AG-UI pin decision documents.

Prerequisites: clean implementation branch and reachable fork ref.

Verification:

```bash
go mod tidy
go list -m -json github.com/ag-ui-protocol/ag-ui/sdks/community/go
go mod download -json github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572
go mod download -json github.com/cloudwego/eino@v0.9.19
go build ./...
```

Gate: stop unless the full origin hashes match the overview. WP2 and WP3 may start in parallel only after compilation succeeds and the semantic failures are recorded.

### WP2 — Adapt conversion and emitter protocol behavior

Changes:

- `convert/convert.go`, `convert/convert_test.go`;
- `convert/usage.go` and `convert/usage_test.go` (`new`);
- `tools/binding.go`, `tools/binding_test.go`;
- `emitter/emitter.go`, `emitter/emitter_test.go`.

Results:

- message/tool-call metadata, non-empty message subagent attribution, and tool-call encrypted continuity survive supported internal round trips under a namespaced Eino key;
- tool-definition metadata survives the one-way `ClientToolInfos` binding path, and explicit-empty message subagent attribution normalizes to absence;
- snapshot emission deep-copies and scrubs every top-level and nested tool-call encrypted value;
- Eino token usage maps without invented zero values;
- reasoning start validates with role `reasoning`;
- the public proposed `Emit` path supports caller-built target events;
- run usage, tool parent IDs, and subagent lifecycle helpers emit valid frames.

Verification:

```bash
go test ./convert ./tools ./emitter -count=1
go test ./emitter -run 'Reasoning|Metadata|Usage|Subagent|Parent|Transport|Encoding' -count=1
```

### WP3 — Preserve stream identity and tool ownership

Changes:

- `stream/stream.go`, `stream/stream_test.go`;
- `internal/testmodel/fixtures.go` and related tests as needed;
- `examples/stream/main.go`.

Prerequisite: WP2's `ToolStart` and reasoning correction. WP3 can proceed alongside WP2's conversion/metadata subwork, but it cannot finalize until the emitter API is stable.

Results:

- proposed `stream.Result` returns assistant output, wire messages, tool owner ID, retained usage, and partial status;
- tool events and transcript share one owner ID;
- interleaved/malformed calls follow the explicit safe-correlation/error policy and do not alias;
- all started blocks close on writable-transport exit paths, while terminal transport errors stop further writes.

Verification:

```bash
go test ./stream ./internal/testmodel -count=1
go test ./stream -run 'StreamTurn|Tool|Owner|Error|Cancel|Usage' -count=20
go test -race ./stream ./emitter
go run ./examples/stream
```

### WP4 — Refresh parity evidence and documentation

Changes:

- `testdata/golden/capture_reference.sh`, `testdata/golden/README.md`, normalized fixtures;
- `internal/golden/normalizer.go`, its tests, and `internal/golden/feature_inventory_test.go` (`new`);
- `parity_test.go`;
- `README.md`, package docs, architecture and decision documents;
- `.github/workflows/*` only if inspection proves a gap.

Prerequisites: WP2 and WP3 behavior is final. Do not regenerate goldens early and then normalize away unexpected differences.

Verification:

```bash
AG_UI_REPO_DIR=/path/to/ag-ui testdata/golden/capture_reference.sh
go test ./... -run Parity -count=1
rg -n 'v0\.8\.13|v0\.9\.2|d2049debabd9|a6dd6fd896ead9' README.md docs testdata go.mod
```

The `rg` results must be limited to clearly historical, superseded context that cannot be misread as current instruction; otherwise update or remove them.

### WP5 — Integration and repository completion

Run:

```bash
go build ./...
make check
go test ./...
go test ./... -run Parity -count=1
go test -race ./convert ./emitter ./stream ./tools
go list -m -json github.com/ag-ui-protocol/ag-ui/sdks/community/go
go mod download -json github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572
go mod download -json github.com/cloudwego/eino@v0.9.19
git diff --check
git status --short
```

Inspect the final diff for unrelated dependency movement, local paths, stale pins, weakened assertions, and generated artifacts. Then follow `AGENTS.md`: update/close Beads issues, commit selectively, `git pull --rebase`, `bd dolt push`, `git push`, and verify the branch is up to date with origin.

## Integration and regression gates

- Module gate: exact fork and Eino hashes resolve reproducibly without a local path at WP1 and again after the final dependency-affecting operation.
- Protocol gate: emitted sequences validate with the target SDK and retain required absent-versus-empty behavior.
- Correlation gate: tool `parentMessageId` equals the returned tool-owner message ID.
- Error gate: encoding errors remain recoverable; transport errors cancel once and halt emission.
- Security gate: encrypted reasoning is still scrubbed, and metadata never becomes prompt/content accidentally.
- Scope gate: classic `ToolCallingChatModel` remains the only Eino model boundary advertised in this release.

## Success-criterion evidence map

| Overview criterion | Required evidence |
| --- | --- |
| 1 exact resolution | WP1 `go list` replacement record plus both `go mod download -json` full-origin records |
| 2 build compatibility | `go build ./...`, compile-only tests, and final full test run |
| 3 reasoning validation | named emitter/stream reasoning tests plus decoded SSE frame assertion |
| 4 tool ownership | stream owner tests plus golden `parentMessageId` relationship |
| 5 complete emitter path | event-family inventory, subagent sequence test, and caller-built metadata/lineage event tests |
| 6 usage lifecycle | converter tests plus end-to-end success, interrupt, model-error, and transport-exclusion tests |
| 7 bridge/security | message/tool-call metadata, non-empty subagent, and encrypted round-trip tests; one-way tool-definition metadata test; explicit-empty subagent normalization test; immutable deep snapshot scrub tests |
| 8 parity and gates | target-commit capture, parity suite, build, `make check`, race suite, and example smoke test |

## Definition of done

- All eight success criteria in `00-overview.md` have the evidence mapped above.
- Exact pins and full origin hashes match the request.
- All standard, parity, race, lint, vet, format, build, and example gates pass.
- The feature inventory distinguishes typed helpers, bridge round trips, one-way bindings, generic emission, direct SDK use, and deferred capabilities.
- Current documentation contains no stale adoption command.
- No implementation claim covers AgenticModel/ADK, non-image multimodal conversion, HTTP routing, persistence, or tool execution.
- All intentional changes are committed and pushed; `git status` reports the branch up to date with origin.

## Deferred work

- Design an Eino `AgenticModel`/ADK-to-AG-UI block and subagent mapping when a concrete consumer exists.
- Add audio/video/document/binary conversion only after provider behavior and error policy are specified.
- Remove the fork replacement if and when the exact SDK content becomes available from the canonical module repository.
- Upgrade downstream applications in separately authorized, repository-specific work.
