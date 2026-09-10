# Package Origins

This library tracks the AG-UI/eino seam from the target AG-UI Go example:

```text
github.com/mattsp1290/ag-ui@aaa75b54d572be8cd1d51c72e951273c5b893ed0
sdks/community/go/example/server/internal/agent/*
```

Decision 0002 keeps consumer-specific orchestration outside this library; the
target AG-UI example is the protocol-behavior reference.

## Extracted Packages

| Package | Reference origin | Library responsibility |
| --- | --- | --- |
| `convert` | `internal/agent/convert.go`, decision 0005 | Preserve the classic bridge and project all Eino v0.9.19 agentic content into a bounded, privacy-safe `eino.agentic.v1` contract. |
| `emitter` | `internal/agent/emitter.go`, decision 0005 | Emit classic events plus receipt-gated agentic native/custom facts. Observer emitters detach without cancelling execution. |
| `stream` | `internal/agent/loop.go:streamTurn`, decision 0005 | Tap classic streams and drain one AgenticModel request or typed ADK source into uncommitted, identified candidates. |
| `tools` | `internal/agent/runconfig.go` | Bind client tools and preserve tool metadata one-way in `ToolInfo.Extra`; classify client/server calls without executing them. |

## Deliberately App-Owned

The library deliberately does not own route, state, persistence, or execution
policy from the reference app. These remain with consuming applications:

- `RunConfig` and route configuration, including whether a route streams live
  tool calls or emits post-turn tool proposals.
- HTTP wiring and SSE transport lifetime management.
- Application state, docstate snapshots, file-read state, and persistent run
  storage.
- Tool execution, approval interrupts, resume behavior, and settlement of
  pending tool calls.
- Post-turn proposal emission for non-live routes. If `stream.StreamTurn` is
  called with live tool-call events enabled, callers must not also emit
  post-turn proposals for the same calls.
- Tool-call validation/correction policy and synthesized missing-ID recovery.
- Activity snapshots and deltas that describe app-specific approval or
  execution progress.
- `agent_complete` and any other custom event semantics tied to app workflows.
- Capability discovery uses AG-UI SDK types directly; this library adds no
  wrapper. The agentic projection observes tool-search, MCP/server-tool, and
  media records but never executes them or fetches assets.

This boundary keeps `eino-agui` focused on the reusable protocol bridge:
AG-UI message/tool structures, AG-UI SSE event emission, and the live eino
stream tap.
