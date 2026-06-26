# Golden Fixtures

These fixtures capture normalized behavior from the current reference app:

- Repository: `github.com/mattsp1290/ag-ui-go-server-example`
- Commit: `a6dd6fd896ead9a06014a8a4bed0bb6a1a6cdfb5`

The comparison contract is structural equivalence after normalization, not
byte-for-byte SSE equality. Runtime-minted SSE frame IDs, event timestamps, and
generated AG-UI message IDs are masked as placeholders. Stable semantic IDs such
as `thread-golden`, `run-golden`, and tool-call IDs are intentionally preserved.

The four fixture files correspond to the first extraction units:

- `convert.normalized.json`: message conversion and provider-gated vision input.
- `emitter.normalized.json`: `MESSAGES_SNAPSHOT` encrypted reasoning scrubbing.
- `stream_turn.normalized.json`: reasoning/text block ordering plus streamed
  tool-call buffering.
- `tool_binding.normalized.json`: client tool binding, classification, and
  client-tool handback.

To re-check the fixtures against the reference implementation, run:

```bash
testdata/golden/capture_reference.sh
```

Set `REFERENCE_APP_DIR` when the reference checkout is not at
`/Users/punk1290/git/ag-ui-go-server-example`.
