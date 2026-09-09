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
| `convert` | `internal/agent/convert.go` | Convert AG-UI messages and tool calls to classic Eino values and back, retaining namespaced metadata, subagent attribution, encrypted continuity, and mapped token usage. Image-only multimodal gating remains explicit. |
| `emitter` | `internal/agent/emitter.go` | Emit typed or caller-built AG-UI events through one error path, including run usage, subagent lifecycle, stable tool parents, and deep snapshot scrubbing. |
| `stream` | `internal/agent/loop.go:streamTurn` | Tap one classic Eino model stream and return `Result` with assistant output, exact wire messages, tool owner, observed usage, and partial state. Safe correlation is internal. |
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
  wrapper. Eino AgenticModel/ADK, MCP/server-tool blocks, tool search, and
  audio/video/document/binary conversion remain outside the supported bridge.

This boundary keeps `eino-agui` focused on the reusable protocol bridge:
AG-UI message/tool structures, AG-UI SSE event emission, and the live eino
stream tap.
