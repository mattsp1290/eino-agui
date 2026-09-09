# Decision 0004: Stream Result and Ownership Boundary

Date: 2026-09-09

## Decision

`stream.StreamTurn` owns the reusable live Eino-to-AG-UI tap and returns a
`stream.Result` containing the concatenated assistant message, exact AG-UI wire
messages, stable tool-owner ID, observed token usage, and partial-state flag.
The tool owner's message ID is also emitted as every live tool call's
`parentMessageId`.

Stream correlation uses a non-nil Eino tool-call index first and a stable,
non-empty ID second. Anonymous fragments are not merged. A changed ID at one
index or an ID claimed by multiple indices returns `CorrelationError` and does
not attach ambiguous calls to the wire transcript.

The stream tap closes writable text, reasoning, and tool blocks on all exit
paths. Once the model stream opens, failures return a non-nil partial result
with complete wire messages and independently observed maximum usage.

## Application boundary

Applications continue to own tool validation and execution, approvals,
interrupt persistence/resume, HTTP routing, and post-turn proposal emission.
Live tool emission and post-turn proposal emission are mutually exclusive.
The library does not synthesize missing IDs or introduce ADK/AgenticModel
orchestration semantics.
