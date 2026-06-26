# Decision 0003: AG-UI Go SDK Pin

Date: 2026-06-26

## Decision

Pin `github.com/ag-ui-protocol/ag-ui/sdks/community/go` to:

```text
v0.0.0-20260624151131-d2049debabd9
```

This pseudo-version resolves the upstream `release/2026-06-24` commit:

```text
d2049debabd9d9b1901c1ba0f5c6321b6c3392cc
```

The slash-containing repository tag name `release/2026-06-24` is not itself a valid Go module
version, so downstream `go.mod` files must use the pseudo-version above.

## Evidence

The reference app currently uses a local replace. This is evidence of the current reference-app
state, not an adoption instruction for committed consumer branches:

```go
replace github.com/ag-ui-protocol/ag-ui/sdks/community/go => /Users/punk1290/git/ag-ui/sdks/community/go
require github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-00010101000000-000000000000
```

The local checkout at `/Users/punk1290/git/ag-ui` is on:

```text
677dfca132c9fdfbca6711a3ecce679300630e4e
Merge pull request #1256 from ag-ui-protocol/fix/issue-1251-reasoning-content
```

That commit is published on the public upstream `main` branch and on the fork remote used locally:

```text
https://github.com/ag-ui-protocol/ag-ui.git refs/heads/main -> 677dfca132c9fdfbca6711a3ecce679300630e4e
https://github.com/mattsp1290/ag-ui.git refs/heads/main -> 677dfca132c9fdfbca6711a3ecce679300630e4e
```

The local SDK subtree has no uncommitted changes:

```text
git -C /Users/punk1290/git/ag-ui diff --exit-code HEAD -- sdks/community/go
sdk_worktree_diff_exit=0
```

The latest release tag differs from current `main` elsewhere in the monorepo, but not under
`sdks/community/go`:

```text
git -C /Users/punk1290/git/ag-ui diff --exit-code \
  release/2026-06-24..677dfca132c9fdfbca6711a3ecce679300630e4e -- sdks/community/go
sdk_diff_exit=0
```

The release tag itself resolves to the selected commit:

```text
git ls-remote https://github.com/ag-ui-protocol/ag-ui.git refs/tags/release/2026-06-24
d2049debabd9d9b1901c1ba0f5c6321b6c3392cc	refs/tags/release/2026-06-24
```

Go module resolution by the release commit succeeds and produces the selected pseudo-version:

```text
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@d2049debabd9
require github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260624151131-d2049debabd9
```

Canonical module metadata:

```json
{
  "Path": "github.com/ag-ui-protocol/ag-ui/sdks/community/go",
  "Version": "v0.0.0-20260624151131-d2049debabd9",
  "Time": "2026-06-24T15:11:31Z",
  "GoVersion": "1.24.4",
  "Sum": "h1:WhVm2j/L/f74LVJyXTbltsbYTIj/ZUg9hXM3R7L4Deo=",
  "GoModSum": "h1:ERAMOexUee4AIuoxksuuGoEcHl3aqLwaazjGwlR9ZCI="
}
```

To refresh this evidence for a future SDK pin:

```bash
git ls-remote https://github.com/ag-ui-protocol/ag-ui.git refs/tags/<tag>
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@<commit>
go list -m -json github.com/ag-ui-protocol/ag-ui/sdks/community/go
```

## Transport Error Prefixes

The SDK currently wraps transport failures in
`sdks/community/go/pkg/encoding/sse/writer.go` with these string prefixes:

```text
SSE write failed
SSE flush failed
```

Until AG-UI exposes a typed or sentinel transport error, `eino-agui` should classify only the outer
SDK write/flush wrappers:

```go
func isTransportError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.HasPrefix(msg, "SSE write failed:") ||
		strings.HasPrefix(msg, "SSE flush failed:")
}
```

The regression test should induce both a write error and a flush error through the pinned
`sse.SSEWriter`, assert those exact colon-suffixed prefixes, and assert that encoding or frame
creation errors are not classified as transport disconnects. If the project upstreams a typed or
sentinel error later, replace string matching with `errors.Is` or an equivalent typed check.

## Consumer Alignment

The consumers covered by this decision are:

- `github.com/mattsp1290/ag-ui-go-server-example`
- The ensemble Go backend that emits AG-UI SSE events, once its real repository path is confirmed

Both consumers should use exactly this SDK version while the shared library is introduced:

```go
require github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260624151131-d2049debabd9
```

Each consumer migration must verify the selected SDK version with:

```bash
go list -m github.com/ag-ui-protocol/ag-ui/sdks/community/go
```

Expected output:

```text
github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260624151131-d2049debabd9
```

During local migration, a temporary sibling checkout replace is acceptable in an uncommitted working
tree:

```go
replace github.com/ag-ui-protocol/ag-ui/sdks/community/go => /Users/punk1290/git/ag-ui/sdks/community/go
```

That replace must not be committed in a published library release. Consumer adoption is not aligned
until each committed consumer branch either uses the pseudo-version directly or has an explicit
consumer-local migration note explaining why a temporary replace remains. The published `eino-agui`
module should carry the pseudo-version require and leave application-local replaces to consumer
development branches only.
