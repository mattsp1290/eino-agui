# Decision 0004: Stream Helper Assignment

Date: 2026-06-26

## Decision

For the first `eino-agui` extraction, extract only the helpers required to make the reusable
`streamTurn` equivalent safe and self-contained. Keep post-turn validation, proposal emission, and
tool settlement in the consuming application.

| Helper | Assignment | Rationale |
| --- | --- | --- |
| `toolCallKey` | Extract, as an unexported implementation detail of the stream tap. | The stream tap needs a stable key across streamed OPEN/delta/CLOSE chunks. The reference app keys by `*schema.ToolCall.Index`, with ID fallback, to buffer tool-call args until both ID and name are known. This is part of the AG-UI streaming event contract. |
| `emitToolProposal` | Stays in app. | This is post-turn proposal emission for routes that do not use live streamed `TOOL_CALL_*` events. It is deliberately mutually exclusive with the stream tap: callers that stream tool calls must not also emit proposals. Keeping it in the app prevents duplicate AG-UI tool events and leaves route policy (`StreamToolCalls`, approval interrupts, client/server split) outside the library. |
| `validateToolCalls` | Stays in app. | It mutates the assistant message, appends corrective `schema.ToolMessage` values to the route conversation, logs local diagnostics, and optionally emits `TOOL_CALL_RESULT`. Those are route and recovery-policy decisions, not generic stream-tap behavior. |
| `validateToolCallsQuiet` | Stays in app. | This is a route-specific variant for surfaces such as shared-state and predictive-state routes that must not leak tool-call events on the wire. That route contract belongs with the app. |
| `settlePendingToolCalls` | Stays in app. | It executes the app's `Deps.Tools`, applies human approval decisions, emits app-specific activity snapshots and state deltas, records file-read state, and threads tool results back into the conversation. It depends on `State`, `Deps`, approval resume semantics, and route-specific UI behavior. |

## Stream Tap Contract

The extracted stream package should own the live stream-to-AG-UI tap:

- consume an eino `StreamReader` / `ToolCallingChatModel.Stream`;
- emit text and reasoning blocks without overlap;
- optionally emit live `TOOL_CALL_START`, `TOOL_CALL_ARGS`, and `TOOL_CALL_END`;
- buffer tool-call argument fragments until a non-empty tool-call ID and function name are known;
- close any open text, reasoning, and streamed tool-call blocks on EOF or error;
- return the concatenated `*schema.Message`.

The package must document this caller contract:

```text
If live tool-call streaming is enabled, callers MUST NOT also emit post-turn tool proposals for
the same calls.
```

That contract is load-bearing: the reference app already has both code paths, and `RunConfig` states
that live streaming and post-turn `emitToolProposal` are mutually exclusive.

## Ensemble Constraint

Decision 0002 found that ensemble does not contain a duplicated AG-UI stream tap. Ensemble has
adjacent concepts (`toolCallKey([]schema.ToolCall)`, synthesized tool-call IDs, a validator, and a
ReAct loop), but they are dispatcher/orchestrator-specific and are not first-extraction public API
requirements.

Therefore this decision does not extract ensemble's validator, dispatcher events, corrective-message
policy, or tool-loop settlement into `eino-agui`.

## Follow-Up Implications

Implementation tasks should:

- put the streaming key/buffer state inside the stream package rather than exposing it as a public
  helper;
- add tests for the no-duplicate-proposal contract by verifying that live-streamed calls are not
  re-emitted by the migrated reference app route;
- leave app-specific validation and settlement in `ag-ui-go-server-example` during the first
  migration;
- revisit a generic validator or tool-loop package only after a separate design task proves multiple
  consumers need the same non-AG-UI policy surface.
