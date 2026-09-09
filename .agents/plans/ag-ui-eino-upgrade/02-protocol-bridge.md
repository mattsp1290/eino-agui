# 02. Protocol Bridge and Emitter

## Goal and prerequisite state

After the exact dependency baseline compiles, adapt the reusable AG-UI/Eino bridge to the target protocol's corrected validation, metadata, run usage, and subagent surface.

## Repository evidence

- `emitter/emitter.go` centralizes transport-versus-encoding failure handling in private `write`.
- `emitter.ReasoningMessageStart` currently constructs role `assistant`; the target SDK validates only role `reasoning` for that event.
- `convert/convert.go` and `tools/binding.go` currently discard message, tool-call, and tool metadata even though Eino v0.9.19 provides `Extra` on `schema.Message`, `schema.ToolCall`, and `schema.ToolInfo`.
- The target SDK adds `BaseEvent.Metadata`, subagent event types, subagent attribution fields, run usage options, run parent/input options, tool parent-message IDs, and public validation/codec helpers.
- The emitter already suppresses malformed-event failures into `EncErr` while continuing later writes and turns transport failures into terminal `Err` plus cancellation. New event paths must preserve that behavior.

## Exact change surface

### Conversion

- `convert/convert.go`: add proposed package-owned constants/helpers for namespaced metadata in Eino `Extra`.
- `convert/convert.go`: update `ToEinoMessages`, `ToEinoToolCalls`, `ToAGUIMessages`, and `ToAGUIToolCalls` to preserve supported metadata, message `SubagentRunID`, and tool-call `EncryptedValue` without aliasing caller maps or pointers.
- `convert/convert_test.go`: add round-trip and collision tests for message and tool-call metadata.
- `tools/binding.go`: copy `types.Tool.Metadata` into `schema.ToolInfo.Extra` under the same namespace.
- `tools/binding_test.go`: verify one-way tool-definition metadata preservation, nil-versus-empty behavior, and caller-map immutability. Do not claim a reverse round trip because no `schema.ToolInfo` to `types.Tool` API exists.
- `convert/usage.go` (`new`, under existing `convert/`): add proposed `ToAGUITokenUsage(usage *schema.TokenUsage, provider, model string) (*events.TokenUsage, error)`.
- `convert/usage_test.go` (`new`): cover all five counts, nil input, all-zero input, provider/model labels, and partial positive counts.

Use one private namespaced key, proposed as `github.com/mattsp1290/eino-agui/ag-ui`, in each Eino `Extra` map. The message envelope contains `metadata` and non-empty `subagentRunId`; the tool-call envelope contains `metadata` and presence-sensitive `encryptedValue`; the tool-info envelope contains `metadata`. Preserve provider-owned keys. On reverse conversion, consume only that key and ignore values of the wrong shape. Copy nested JSON maps/slices and string pointers sufficiently to prevent mutations on the returned value from changing the input. Add explicit collision and empty-versus-absent tests for every envelope field. Normalize an explicit empty message `subagentRunId` to absence because the target SDK tracks its presence in an unexported field that this library cannot reconstruct through public APIs; test and document that normalization.

For usage mapping:

| Eino v0.9.19 | AG-UI target |
| --- | --- |
| `PromptTokens` | `InputTokens` |
| `CompletionTokens` | `OutputTokens` |
| `TotalTokens` | `TotalTokens` |
| `CompletionTokensDetails.ReasoningTokens` | `ReasoningTokens` |
| `PromptTokenDetails.CachedTokens` | `CachedInputTokens` |

Return `(nil, nil)` when Eino supplies no usage or all counts are zero. Set a pointer only for a positive count because classic Eino values cannot distinguish an absent count from a reported zero. Return `(nil, error)` when any count is negative; never construct an event that target validation rejects.

### Emitter

- `emitter/emitter.go`: rename/export private `write` as proposed `Emit(events.Event) bool`, keeping one implementation for all typed helpers.
- `emitter/emitter.go`: make both a nil interface and an interface containing a typed nil event pointer non-panicking encoding errors and preserve first-error semantics. Detect typed nil before invoking any event method or the SDK writer.
- `emitter/emitter.go`: correct `ReasoningMessageStart` to role `reasoning`.
- `emitter/emitter.go`: change proposed `ToolStart(toolCallID, name, parentMessageID string)` to apply `events.WithParentMessageID` only when the parent ID is non-empty.
- `emitter/emitter.go`: extend `RunFinishedSuccess`, `RunFinishedInterrupt`, and `RunError` with variadic `events.TokenUsage` so zero-usage call sites remain concise while the new wire field is available. Do not attempt a run-ending frame after a terminal SSE transport failure.
- `emitter/emitter.go`: add proposed `SubagentStarted`, `SubagentFinished`, and `SubagentError` typed methods that accept the corresponding target-SDK option types and delegate through `Emit`.
- `emitter/emitter.go`: replace shallow snapshot scrubbing with a deep-enough copy that clears top-level `Message.EncryptedValue`, `Message.EncryptedContent`, and every `Message.ToolCalls[*].EncryptedValue` while retaining non-sensitive metadata and leaving caller values unchanged.
- `emitter/emitter_test.go`: add target-SDK protocol validation, metadata, usage, subagent sequence, tool parent, nil-event, encoding-error continuation, and transport-error cancellation tests.

Caller-built events are the supported path for capabilities that require per-event fields not known by this library, including `BaseEvent.Metadata`, normal-event `SubagentRunID`, `RUN_STARTED` parent/input, and specialized results/outcomes. Document a minimal `emit.Emit(event)` example. Do not duplicate every SDK option in this library.

## Intended behavior and error paths

- Every typed method and caller-built event passes through exactly one `Emit` path.
- A target-SDK validation or encoding failure sets only the first `EncErr`, drops that event, and permits a later valid event.
- A write or flush failure sets only the first terminal `Err`, invokes cancellation once, and makes later emissions no-ops.
- Typed lifecycle helpers produce sequences accepted by `events.ValidateSequence`.
- Message and tool-call metadata round-trip only where Eino has an explicit `Extra` carrier; tool-definition metadata is one-way through `ClientToolInfos`. Metadata must not be injected into prompts or visible content.
- Encrypted reasoning values remain available only in the internal conversion envelope. `MESSAGES_SNAPSHOT` recursively scrubs top-level and nested tool-call encrypted values without mutating the input; metadata handling must not copy encrypted values into the metadata member.
- Empty state/activity deltas retain the library's no-op convenience behavior even though the target SDK can serialize empty arrays.

## Verification and acceptance

Run:

```bash
go test ./convert ./tools ./emitter -count=1
go test ./emitter -run 'Reasoning|Metadata|Usage|Subagent|Parent|Transport|Encoding' -count=1
go test ./... -run Parity -count=1
```

Acceptance requires decoded SSE JSON assertions for `metadata`, `usage`, non-empty message/event `subagentRunId`, explicit-empty subagent normalization, `parentRunId` via a caller-built event, and `parentMessageId`. Snapshot tests must include multiple tool calls with encrypted values and metadata, assert that no encrypted value reaches the wire, and prove the inputs were not mutated. Generic emission tests must cover both a nil event interface and a typed nil event pointer. At least one test must pass the emitted concrete events through `events.ValidateSequence`, not merely compare selected JSON keys.

## Dependencies, risks, and exclusions

- This package depends on the exact target SDK because its option types and validation semantics are part of the public surface.
- `Emit` is intentionally low-level. Document that callers own protocol sequencing for events they build.
- Do not add capability-discovery wrappers. Consumers should use `types.AgentCapabilities` directly.
- Do not add Eino AgenticModel conversion here. That requires a separately evidenced model/block mapping.
- Do not expand current image-only input forwarding to audio, video, document, or generic binary content in this upgrade.
