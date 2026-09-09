# 01. Dependency Baseline

## Goal and prerequisites

Establish reproducible exact pins and make the existing source compile and test far enough to expose semantic protocol failures. Start from a clean branch based on `main` and preserve unrelated work.

## Repository evidence

- Existing pins live in `go.mod`; the dependency graph is recorded in `go.sum`.
- `README.md` and `docs/decisions/0001-eino-version-floor.md` describe the old Eino floor.
- A temporary copy successfully resolved the fork as `github.com/mattsp1290/ag-ui/sdks/community/go v0.0.0-20260909025854-aaa75b54d572` when used as a replacement for the canonical import path.
- The same copy resolved `github.com/cloudwego/eino v0.9.19` and compiled all packages. Tests then exposed the reasoning-role validation failure, proving that compilation alone is insufficient.

## Exact change surface

- `go.mod`: change both direct requirements and add the exact fork replacement.
- `go.sum`: regenerate through `go mod tidy`; do not hand-edit checksums.
- `docs/decisions/0001-eino-version-floor.md`: replace the old v0.8.13/v0.9.2 compatibility policy with a new dated decision for the exact v0.9.19 floor, or add `docs/decisions/0005-eino-v0.9.19.md` (`new`, under existing `docs/decisions/`) that explicitly supersedes Decision 0001.
- `docs/decisions/0003-agui-sdk-pin.md`: supersede its old commit/pseudo-version and explain why the fork `replace` is required.
- `internal/deps/version_test.go` (`new`, under existing `internal/deps/`): add compile-time/API-contract coverage for the target symbols used by later packages. Do not attempt network resolution inside a Go test.

Required module form:

```go
require (
    github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260909025854-aaa75b54d572
    github.com/cloudwego/eino v0.9.19
)

replace github.com/ag-ui-protocol/ag-ui/sdks/community/go => github.com/mattsp1290/ag-ui/sdks/community/go v0.0.0-20260909025854-aaa75b54d572
```

## Intended behavior and invariants

- Source imports remain canonical `github.com/ag-ui-protocol/ag-ui/...` imports. Only module resolution points at the fork.
- No committed path may refer to `/Users/...`, `~/git/...`, or another local checkout.
- `go mod tidy` must not upgrade either direct target beyond the requested version.
- The target AG-UI full origin hash must remain `aaa75b54d572be8cd1d51c72e951273c5b893ed0`.
- The target Eino tag must resolve to `9d983b36a5112a1c233056b1a099825298fafb8f`.

## Verification and acceptance

Run:

```bash
go mod tidy
go list -m -json github.com/ag-ui-protocol/ag-ui/sdks/community/go
go mod download -json github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572
go mod download -json github.com/cloudwego/eino@v0.9.19
go build ./...
go test ./... -run '^$'
git diff --check
```

Acceptance requires the canonical module's `Replace` record to show the target fork path and pseudo-version. The fork download record must show `Origin.URL` `https://github.com/mattsp1290/ag-ui`, `Origin.Subdir` `sdks/community/go`, and full `Origin.Hash` `aaa75b54d572be8cd1d51c72e951273c5b893ed0`. The Eino download record must show full `Origin.Hash` `9d983b36a5112a1c233056b1a099825298fafb8f` and the `v0.9.19` tag ref. Record these commands in the new decision document without checkout-specific cache paths.

Expected first semantic failure: existing emitter/stream tests can fail until Work Package 2 corrects the reasoning role and fixtures. Unexpected compile failures or failures outside documented protocol changes stop this package and must be explained before later work starts.

## Risks and exclusions

- A pseudo-version's short suffix is not proof of the full origin hash; inspect the module's `Origin.Hash`.
- Do not vendor either dependency.
- Do not retain the old v0.8.13 compatibility matrix. The user explicitly chose no backward compatibility.
- Do not update unrelated direct or tool dependencies unless `go mod tidy` requires an indirect graph change; inspect and explain every such change.
