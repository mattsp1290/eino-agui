# Golden Fixtures

These fixtures capture normalized behavior from the AG-UI Go SDK example:

- Repository: `github.com/mattsp1290/ag-ui`
- Commit: `aaa75b54d572be8cd1d51c72e951273c5b893ed0`
- Subtree: `sdks/community/go/example/server/internal/agent`

The comparison contract is structural equivalence after normalization, not
byte-for-byte SSE equality. Runtime-minted SSE frame IDs, event timestamps, and
generated AG-UI message IDs are masked as placeholders. Stable semantic IDs such
as `thread-golden`, `run-golden`, and tool-call IDs are intentionally preserved.

The classic fixture files correspond to the first extraction units:

- `convert.normalized.json`: message conversion and provider-gated vision input.
- `emitter.normalized.json`: `MESSAGES_SNAPSHOT` encrypted reasoning scrubbing.
- `stream_turn.normalized.json`: reasoning/text block ordering plus streamed
  tool-call buffering.
- `tool_binding.normalized.json`: client tool binding, classification, and
  client-tool handback.

The agentic fixtures extend that baseline without redefining classic parity:

- `agentic_convert.normalized.json`: exact 20-kind and five nested-kind public
  inventory plus private-field exclusions.
- `agentic_stream.normalized.json`: identified transient/native and committed
  `eino.agentic.v1` supplement shapes.

Agentic clients merge native rendering events with custom supplements only on
the complete session/run/turn/message/block/attempt/agent-path identity tuple.
The custom content block is authoritative after commit and is not appended as
a duplicate logical block. Replay starts with a fresh reducer and consumes the
complete canonical native sequence once.

To re-check the four classic fixtures against the reference implementation,
run:

```bash
AG_UI_REPO_DIR=/path/to/ag-ui testdata/golden/capture_reference.sh
```

The script requires an exact, clean checkout of the target commit and removes
its injected capture test on both success and failure.

The agentic fixtures are bridge-owned because the pinned AG-UI example does not
implement Eino's agentic schema. Rebuild and strictly compare the normalized
agentic stream fixture, then exercise its native/custom merge through the exact
pinned TypeScript reducer, with:

```bash
go test ./emitter ./internal/golden -run 'AgenticStream|AgenticConvert' -count=1
AG_UI_REPO_DIR=/path/to/ag-ui testdata/agui-client/check.sh
```
