# Decision 0003: AG-UI Go SDK Fork Pin

Date: 2026-09-09

## Decision

Keep canonical AG-UI imports while resolving the module through the exact fork
commit requested for this release:

```go
require github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260909025854-aaa75b54d572

replace github.com/ag-ui-protocol/ag-ui/sdks/community/go => github.com/mattsp1290/ag-ui/sdks/community/go v0.0.0-20260909025854-aaa75b54d572
```

The commit is not reachable from the canonical repository, so querying that
module directly reports an unknown revision. The committed remote-module
replacement is reproducible and does not depend on a local checkout.

## Evidence

```bash
go list -m -json github.com/ag-ui-protocol/ag-ui/sdks/community/go
go mod download -json github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572
```

The replacement origin must be `https://github.com/mattsp1290/ag-ui`, subtree
`sdks/community/go`, full hash
`aaa75b54d572be8cd1d51c72e951273c5b893ed0`.

The SDK still identifies transport errors by the outer `SSE write failed:` and
`SSE flush failed:` prefixes. Encoding/validation errors remain recoverable;
transport failures cancel once and stop later emission. Remove the replacement
only after the same content is reachable and verified under the canonical
module path.
