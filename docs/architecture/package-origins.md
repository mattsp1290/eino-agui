# Package Origins

This library extracts the AG-UI/eino seam from the reference app:

```text
github.com/mattsp1290/ag-ui-go-server-example
internal/agent/*
```

The first extraction is reference-app-derived. Decision 0002 records that
`ensemble` is an adjacent eino consumer, not an existing AG-UI SSE consumer, so
it is not used as proof of the AG-UI public API shape.

## Extracted Packages

| Package | Reference origin | Library responsibility |
| --- | --- | --- |
| `convert` | `internal/agent/convert.go` | Convert AG-UI `types.Message` histories to eino `schema.Message` values and back for snapshots. This includes provider-controlled vision gating, multimodal image parts, message text extraction, and tool-call conversion in both directions. |
| `emitter` | `internal/agent/emitter.go` | Emit typed AG-UI SSE events through the AG-UI SDK's concrete `*bufio.Writer` plus `*sse.SSEWriter` pair. This owns lifecycle, text, reasoning, tool, state, message snapshot, activity, step, and custom event helpers; transport-vs-encoding error handling; block closing before tool events; and encrypted reasoning scrubbing for `MESSAGES_SNAPSHOT`. |
| `stream` | `internal/agent/loop.go:streamTurn` | Tap one eino model stream, emit AG-UI reasoning/text/tool-call deltas as chunks arrive, optionally stream live `TOOL_CALL_*` events, close open blocks, and return the concatenated assistant `*schema.Message`. Tool-call buffering is an unexported implementation detail. |
| `tools` | `internal/agent/runconfig.go` | Convert AG-UI client tool definitions to eino `ToolInfo` values, convert client-provided JSON Schema data safely, and split model tool calls into client-owned and server-owned calls. |

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

This boundary keeps `eino-agui` focused on the reusable protocol bridge:
AG-UI message/tool structures, AG-UI SSE event emission, and the live eino
stream tap.
