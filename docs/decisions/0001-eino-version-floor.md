# Decision 0001: Eino v0.9.19 Floor

Date: 2026-09-09

## Decision

The supported Eino version is exactly `github.com/cloudwego/eino v0.9.19` for
this release. The earlier dual-version compatibility policy is superseded.
There are no active consumers requiring the older API, so the public stream
result may evolve without a compatibility shim.

The classic `model.ToolCallingChatModel` and `schema.Message` boundary remains
the supported integration. Eino's `AgenticModel`, ADK runtime, tool search, and
richer agentic blocks are deliberately outside this release because no current
consumer establishes their AG-UI lifecycle semantics.

## Evidence

The tag resolves reproducibly with:

```bash
go mod download -json github.com/cloudwego/eino@v0.9.19
```

The required origin is `https://github.com/cloudwego/eino`, ref
`refs/tags/v0.9.19`, hash
`9d983b36a5112a1c233056b1a099825298fafb8f`.

Eino's integer token counters cannot distinguish “absent” from “reported
zero.” The bridge therefore maps only positive counts and never fabricates
zero-valued AG-UI pointers.
