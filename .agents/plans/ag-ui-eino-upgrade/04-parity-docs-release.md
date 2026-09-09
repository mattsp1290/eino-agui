# 04. Parity, Documentation, and Release Readiness

## Goal and prerequisite state

After protocol and stream behavior is green, replace stale provenance with target-commit evidence, prove the selected feature matrix, and prepare a releasable change without publishing a tag during implementation unless separately requested.

## Repository evidence

- `testdata/golden/README.md` and `capture_reference.sh` still pin `ag-ui-go-server-example` commit `a6dd6fd896ead9a06014a8a4bed0bb6a1a6cdfb5`.
- The requested reference is now the Go SDK/example subtree at exact `mattsp1290/ag-ui` commit `aaa75b54d572be8cd1d51c72e951273c5b893ed0`.
- `parity_test.go` verifies selected named tests ran, but does not inventory the target SDK's new protocol families.
- `README.md`, package docs, architecture notes, and old decisions advertise Eino v0.8.13 and the old AG-UI pin.

## Exact change surface

### Golden and contract coverage

- `testdata/golden/capture_reference.sh`: change the reference contract to a caller-supplied `AG_UI_REPO_DIR` checkout at the exact target commit. Inject or run capture tests under `sdks/community/go/example/server/internal/agent` without leaving files behind on success or failure. Continue using non-interactive `rm -f` cleanup.
- `testdata/golden/README.md`: record the new repository, full commit, subtree, normalization rules, and capture command. Use environment-variable paths rather than a user-specific default.
- `testdata/golden/*.normalized.json`: regenerate only after inspecting semantic diffs. Expected diffs include reasoning role and tool `parentMessageId`; generated IDs and timestamps remain normalized.
- `internal/golden/normalizer.go` and tests: normalize new nondeterministic IDs while preserving stable parent/child equality. Do not mask `parentMessageId`, `subagentRunId`, metadata, usage, or sequence errors so thoroughly that a broken relationship passes.
- `parity_test.go`: add named parity cases for metadata, usage, subagent lifecycle/attribution, and stream owner correlation.
- `internal/golden/feature_inventory_test.go` (`new`, under existing `internal/golden/`): encode the disposition table from the overview as a maintained inventory. Each target event family and newly relevant message/tool-call field, including `SubagentRunID` and `EncryptedValue`, must be marked `typed-helper`, `bridge-round-trip`, `one-way-binding`, `generic-emit`, `direct-sdk`, or `out-of-scope`, with a linked test name for supported library behavior.

The inventory is not required to wrap all SDK APIs. It prevents “feature support” from becoming an untestable claim.

### Documentation

- `README.md`: update exact version expectations, explain the fork replacement, show the new stream result, show `Emitter.Emit`, and demonstrate usage/subagent emission at a compact level.
- `convert/doc.go`, `emitter/doc.go`, `stream/doc.go`, and `tools/doc.go`: update package boundaries and target semantics.
- `docs/architecture/package-origins.md`: update the reference origin to the AG-UI target commit/subtree and describe the richer stream result plus generic emission boundary.
- `docs/decisions/0001-eino-version-floor.md` or its superseding decision: remove active instructions to test v0.8.13/v0.9.2.
- `docs/decisions/0003-agui-sdk-pin.md`: record the new source and replacement mechanism.
- `docs/decisions/0004-stream-helper-assignment.md`: update the stream-result and parent-message decision without pulling app-owned settlement into the library.

### CI and release readiness

- `.github/workflows/*`: inspect existing Go setup and cache keys after `go.mod` changes. Modify only if current jobs do not execute the required commands or cannot resolve the fork.
- `Makefile`: retain `make check`; add no redundant target unless a repeatable parity or race command materially improves CI.
- Do not create a git tag or GitHub release in this work. Record the likely next release as a breaking pre-1.0 minor (`v0.2.0`) in handoff notes, subject to the repository's release process.

## Verification and acceptance

Run the target capture from a clean checkout:

```bash
AG_UI_REPO_DIR=/path/to/ag-ui testdata/golden/capture_reference.sh
git diff -- testdata/golden
```

Then run:

```bash
go build ./...
make check
go test ./...
go test ./... -run Parity -count=1
go test -race ./convert ./emitter ./stream ./tools
go run ./examples/stream
git diff --check
```

Acceptance requires:

- golden provenance names the full requested commit;
- every supported disposition has an executing test;
- every deferred capability is explicitly named and not advertised as supported;
- all documentation uses the new exact pins and contains no live v0.8.13/v0.9.2 instruction;
- `rg -n '/Users/|~/git' README.md docs testdata .github go.mod` finds no newly introduced checkout-specific instruction;
- repository status contains only intentional implementation, documentation, and regenerated dependency changes.

## Risks and exclusions

- Capturing from a dirty upstream checkout can produce false evidence. The script must verify `HEAD` and relevant subtree cleanliness before injection.
- Structural normalization must preserve relationships, not merely replace every identifier with one placeholder.
- Target SDK capabilities and Eino AgenticModel existence are not evidence that this bridge supports them. Keep the feature inventory honest.
- Release publication, consumer adoption, and downstream repository upgrades are separate authorized actions and remain out of scope.
