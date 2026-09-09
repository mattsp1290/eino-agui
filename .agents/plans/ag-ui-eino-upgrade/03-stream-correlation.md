# 03. Stream Correlation

## Goal and prerequisite state

After the emitter accepts target events, make the Eino v0.9.19 stream tap preserve AG-UI message identity, correlate tool calls to their owner, expose observed usage, and balance lifecycle blocks on every exit while the transport remains writable.

## Repository evidence

- `stream.StreamTurn` currently returns only `*schema.Message`.
- Current streaming creates independent IDs for text/reasoning blocks but does not return their corresponding AG-UI messages.
- Current live tool calls are keyed by index/ID and buffered until ID and name are known, but `toolCallKey` falls back to one constant (`p0`) for every identity-less call. Eino defines `Index` as the streaming correlation key and supplies no safe general association rule for fragments that lack both index and ID.
- The target Go example's loop returns an internal result containing `Assistant`, `WireMessages`, and `ToolOwnerID`; it passes the owner ID as `parentMessageId` on `TOOL_CALL_START` and records reasoning/text blocks in a wire transcript.
- The target example also rejects a provider changing a tool-call ID at one stream index or reusing one ID across indices. The current library does not detect either ambiguity.
- Eino v0.9.19 `schema.ConcatMessages` retains final `ResponseMeta.Usage` using maximum observed cumulative counts.

## Exact change surface

- `stream/stream.go`: add exported `Result` (`new` symbol in existing file) with:

  ```text
  Assistant    *schema.Message
  WireMessages []types.Message
  ToolOwnerID  string
  Usage        *schema.TokenUsage
  Partial      bool
  ```

- `stream/stream.go`: change `StreamTurn` to return `(*Result, error)` and update its documentation.
- `stream/stream.go`: accumulate visible text and reasoning into `WireMessages` using the same IDs emitted on the wire.
- `stream/stream.go`: create one assistant tool-owner message when any tool-call chunk appears, whether live tool events are enabled or not.
- `stream/stream.go`: after `schema.ConcatMessages`, attach converted final tool calls to the owner message and expose the concatenated Eino message as `Result.Assistant`.
- `stream/stream.go`: pass `ToolOwnerID` to the revised `emitter.ToolStart`.
- `stream/stream.go`: remove the constant anonymous-key fallback. Correlate by non-nil index first and stable non-empty ID second. A nil-index entry can live-emit only after that same entry supplies a non-empty ID and name; discard earlier fragments that lacked both index and ID because they cannot be attributed safely. Never merge separate anonymous entries by arrival order.
- `stream/stream.go`: add proposed exported `CorrelationError` (`new` symbol in existing file). Return it with a partial result if one index changes ID or one ID is claimed by multiple indices. Keep raw received chunks out of `WireMessages`; `Result.Assistant` may contain them only if Eino concatenation succeeds without contradicting the correlation error.
- `stream/stream.go`: accumulate maximum observed Eino usage counts independently of `schema.ConcatMessages`, so `Result.Usage` is available on clean EOF, context cancellation, receive error, correlation error, concatenation error, and emitter failure after the model stream opens.
- `stream/stream_test.go`: update all callers and add wire transcript, owner, ambiguity, multi-call, usage-preservation, and exit-balance tests.
- `examples/stream/main.go`: use `result.Assistant`; also demonstrate owner correlation in decoded output or a concise stderr summary.
- `internal/testmodel/fixtures.go` and related tests: extend fixtures only where needed for response usage and malformed/ambiguous tool streams.

All proposed symbols are anchored in the existing `stream` package. No new package is required.

## Intended lifecycle and invariants

```text
reasoning chunk(s)
  -> REASONING_START / MESSAGE_START(role=reasoning) / CONTENT*
  -> MESSAGE_END / REASONING_END
  -> wire reasoning message with identical ID

text chunk(s)
  -> TEXT_MESSAGE_START(role=assistant) / CONTENT* / END
  -> wire assistant message with identical ID

tool chunk(s)
  -> create one empty assistant owner message
  -> TOOL_CALL_START(parentMessageId=owner ID) / ARGS* / END
  -> place concatenated tool calls on the same owner message
```

- Text, reasoning, and tool blocks never overlap.
- A reopened text or reasoning block gets a fresh ID.
- Every successfully emitted start is balanced by an end on EOF, context cancellation, model receive error, correlation error, or concatenation error while the transport remains writable.
- On terminal emitter transport failure, retain the first error, cancel once, perform internal state cleanup, and attempt no later writes. Wire balance is not guaranteed after the connection fails.
- Once the model stream opens, `StreamTurn` returns a non-nil `Result` even with an error. Set `Partial=true`, retain complete wire messages and independently accumulated usage, and set `Assistant` only when received chunks concatenate successfully. If opening the stream fails, return `nil, err`.
- Live tool emission and post-turn proposal emission remain mutually exclusive.
- Empty model streams remain errors.
- Tool arguments received before ID/name are emitted once, in arrival order, after the start event becomes valid.
- A changed ID at one index or a duplicate ID across indices fails the turn with `CorrelationError`; do not attach those calls to the AG-UI owner message. A call with an empty ID is retained only in `Result.Assistant` if Eino concatenation accepts it, is excluded from `WireMessages`, and emits no live event. A nil-index call with a stable ID can correlate by ID; an ID-late nil-index call starts only from the first attributable entry and never absorbs earlier anonymous arguments.
- Callers may emit `RunError` with `Result.Usage` after non-transport failures. A normal result that later becomes an application interrupt may emit `RunFinishedInterrupt` with the same usage. A terminal SSE transport failure cannot carry a reliable ending event.

## Tests and observable acceptance

Add table-driven tests for:

1. reasoning → text → reasoning → tool ordering;
2. two interleaved indexed tool calls with delayed IDs/names;
3. repeated ID across two indices;
4. changed ID on one index;
5. batching permutations for nil-index/no-ID, nil-index/stable-ID, and ID-late entries, proving anonymous fragments never merge accidentally;
6. EOF, cancellation, receive error, and transport error with open blocks;
7. live-tool disabled behavior and returned owner message;
8. exact `parentMessageId` equality across event and `WireMessages`;
9. usage retention in `Result.Usage` on clean EOF, cancellation, receive error, correlation error, and concatenation error;
10. end-to-end conversion and decoded run-ending usage for success, application interrupt, and model receive failure, with terminal transport failure explicitly excluded.

Run:

```bash
go test ./stream ./internal/testmodel -count=1
go test ./stream -run 'StreamTurn|Tool|Owner|Error|Cancel|Usage' -count=20
go run ./examples/stream
```

The repeated test is a determinism check, not a race substitute. Also run `go test -race ./stream ./emitter` in the integration gate.

## Dependencies, risks, and exclusions

- Work depends on the revised `emitter.ToolStart` and corrected reasoning role.
- The `Result` change is a public breaking change authorized by the application context.
- The emitter is not documented as safe for concurrent calls. Do not imply concurrent subagent streaming support; callers must serialize events unless a separate concurrency design is implemented.
- Do not absorb app-owned tool validation, execution, approvals, resume, or persistence from the target example.
- Do not rewrite the classic stream around `model.AgenticModel`; retain `model.ToolCallingChatModel` as the supported model boundary for this release.
