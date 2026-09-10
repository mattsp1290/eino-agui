# eino-agui

Reusable AG-UI helpers for CloudWeGo eino agents.

This module packages the AG-UI/eino seam exercised by the AG-UI Go SDK example
into small packages for:

- AG-UI message conversion: `convert`
- typed AG-UI SSE event emission: `emitter`
- live eino stream tapping: `stream`
- AG-UI client tool binding and classification: `tools`
- bounded Eino v0.9.19 agentic projection and typed ADK observation across
  `convert`, `emitter`, and `stream`

See [docs/architecture/package-origins.md](docs/architecture/package-origins.md)
for the extraction boundary and the app-owned behavior that deliberately stays
outside this library.

## Install

```bash
go mod edit -replace github.com/ag-ui-protocol/ag-ui/sdks/community/go=github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572
go get github.com/mattsp1290/eino-agui@<tag-or-commit>
```

For local development against a checkout:

```bash
go mod edit -replace github.com/ag-ui-protocol/ag-ui/sdks/community/go=github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572
go mod edit -replace github.com/mattsp1290/eino-agui=/path/to/eino-agui
go get github.com/mattsp1290/eino-agui
```

Remove only the local library replacement before depending on a published
version; the remote AG-UI replacement remains required:

```bash
go mod edit -dropreplace github.com/mattsp1290/eino-agui
go get github.com/mattsp1290/eino-agui@<new-tag-or-commit>
```

## Version Expectations

The module currently targets:

- Go `1.26.3`
- `github.com/cloudwego/eino v0.9.19`
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go`
  `v0.0.0-20260909025854-aaa75b54d572`, resolved from the exact
  `github.com/mattsp1290/ag-ui` fork commit through `go.mod`.

The AG-UI commit is not reachable from the canonical repository. Because Go
module replacements are not transitive, a consuming module using this version
must carry the same remote replacement:

```bash
go mod edit -replace github.com/ag-ui-protocol/ag-ui/sdks/community/go=github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572
```

Do not replace it with a filesystem path or a newer fork commit.

## Emitter Contract

`emitter.NewEmitter` intentionally takes the concrete writer pair used by the
AG-UI Go SDK:

```go
writer := bufio.NewWriter(out)
emit := emitter.NewEmitter(ctx, writer, sse.NewSSEWriter(), threadID, runID, cancel)
```

The constructor does not accept a bare `io.Writer`. Callers own wrapping their
HTTP or test output writer in `*bufio.Writer` and providing the SDK
`*sse.SSEWriter`.

Typed helpers cover common lifecycle events. `Emit` is the forward-compatible
path for caller-built SDK events carrying metadata, subagent attribution,
parent run IDs, or input:

```go
started := events.NewRunStartedEventWithOptions(threadID, runID,
    events.WithParentRunID(parentRunID),
    events.WithRunInput(input),
)
started.Metadata = types.Metadata{"trace": traceID}
emit.Emit(started)

emit.SubagentStarted(subagentRunID, "research")
emit.SubagentFinished(subagentRunID, events.WithSubagentSuccessOutcome())
emit.RunFinishedSuccess(events.TokenUsage{
    Provider: "openai", Model: modelName, TotalTokens: events.TokenCount(42),
})
```

Callers using `Emit` own protocol sequencing. Emitter calls must be serialized.

## Stream Result

`stream.StreamTurn` returns a richer result so protocol identity is not lost:

```go
result, err := stream.StreamTurn(ctx, emit, chatModel, messages,
    stream.WithLiveToolCallEvents(true))
if result != nil {
    assistant := result.Assistant
    wireMessages := result.WireMessages
    ownerID := result.ToolOwnerID
    usage := result.Usage
    _ = assistant
    _ = wireMessages
    _ = ownerID
    _ = usage
}
```

After the model stream opens, errors include a non-nil result with
`Partial=true`. Live tool events carry `parentMessageId` equal to the owner
message in `WireMessages`. Callers that enable live tool events must not emit
the same calls again as post-turn proposals.

`convert.ToAGUITokenUsage` maps Eino usage for successful, interrupted, or
non-transport error endings. Only positive counts are emitted because classic
Eino usage cannot distinguish an absent count from an explicit zero.

See [examples/stream](examples/stream) for a minimal runnable example that
streams a deterministic eino model into AG-UI SSE events.

Run the example as a local smoke test:

```bash
go run ./examples/stream
```

It writes AG-UI SSE frames to stdout and the final assistant content plus tool
owner ID to stderr.

## Agentic Runtime Bridge

The rich path accepts `schema.AgenticMessage` and `model.AgenticModel` directly:

```go
emit := emitter.NewObserverEmitter(observerCtx, writer, sseWriter)
observer := emitter.NewObserverSink(emit)
candidate, err := stream.StreamAgenticTurn(ctx, agenticModel, messages, ids, blockResolver,
    stream.WithTransientSink(observer),
)
if err == nil {
    // The host commits candidate.PublicProjection and its digest first.
    emit.EmitCommittedProjection(candidate.PublicProjection, receipt,
        emitter.DeliveryModeLiveContinuation)
}
```

For typed ADK streams, wrap the raw iterator with abort and wait hooks connected
to the same producer, then call `stream.DrainAgenticEvents`. Observer failure
detaches only that sink; execution cancellation comes from the host context.
The event resolver supplies durable pause IDs and optional separately validated
MCP approval correlation. If context cancellation should yield a committed
candidate, pass the host-decided identity and classification through
`stream.WithAgentEventCancellationCandidate`.

All authoritative projections and lifecycle facts require a matching
host-created `convert.CommitReceiptV1`. The bridge does not persist sessions,
execute tools, own checkpoints, authorize approvals, retry model calls, or infer
run completion. `TURN_FINISHED` and pause are nonterminal; committed run endings
require explicit `loopSettled=true`.

Each receipt is consumed at most once by an emitter, preventing duplicate native
content on one connection. A fresh emitter may use the same receipt for replay
after reconnect. Committed projections emit a `response_meta` custom supplement
when token usage, provider terminal details, or Gemini grounding are present.
Committed pause IDs are unique within a run, and approval correlation must match
the paused target exactly when that target resumes.

User-role projections expose a sanitized `NativeMessage` for host-assembled
full transcript snapshots. Model input slices and their pointed-to messages are
read-only for the duration of `StreamAgenticTurn`; the bridge copies the slice
and model-option list and never mutates either.

The only custom namespace is `eino.agentic.v1`. It carries durable identity and
the rich semantics absent from the pinned AG-UI SDK while native AG-UI text,
reasoning, function-tool, run, and subagent events remain available. Private
provider continuation fields, encrypted reasoning, arbitrary extensions, and
ADK checkpoint state are excluded.

See [the complete agentic contract](docs/architecture/agentic-contract.md) for
the 20-kind mapping, exact constructors, limits, receipt rules, and ownership
boundary.

## Local Checks

Install `goimports` once:

```bash
go install golang.org/x/tools/cmd/goimports@v0.47.0
```

Run the standard local gate:

```bash
make check
go test ./...
```

The full validation set used by CI and parity work is:

```bash
go build ./...
make check
go test ./...
go test -race ./convert ./emitter ./stream ./tools
go test ./... -run Parity -count=1
```

`make check` runs:

- `gofmt`/`goimports` format checks
- `go vet ./...`
- `golangci-lint run ./...` through `go run`, pinned to `v2.12.2`
