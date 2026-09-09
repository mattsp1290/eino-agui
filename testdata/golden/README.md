# Golden Fixtures

These fixtures capture normalized behavior from the AG-UI Go SDK example:

- Repository: `github.com/mattsp1290/ag-ui`
- Commit: `aaa75b54d572be8cd1d51c72e951273c5b893ed0`
- Subtree: `sdks/community/go/example/server/internal/agent`

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
AG_UI_REPO_DIR=/path/to/ag-ui testdata/golden/capture_reference.sh
```

The script requires an exact, clean checkout of the target commit and removes
its injected capture test on both success and failure.
