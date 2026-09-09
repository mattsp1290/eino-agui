# eino-agui

Reusable AG-UI helpers for CloudWeGo eino agents.

This module packages the AG-UI/eino seam exercised by the AG-UI Go SDK example
into small packages for:

- AG-UI message conversion: `convert`
- typed AG-UI SSE event emission: `emitter`
- live eino stream tapping: `stream`
- AG-UI client tool binding and classification: `tools`

See [docs/architecture/package-origins.md](docs/architecture/package-origins.md)
for the extraction boundary and the app-owned behavior that deliberately stays
outside this library.

## Install

```bash
go get github.com/mattsp1290/eino-agui
```

For local development against a checkout:

```bash
go mod edit -replace github.com/mattsp1290/eino-agui=/path/to/eino-agui
go get github.com/mattsp1290/eino-agui
```

Remove the local replacement before depending on a published version, and use
an explicit tag or commit:

```bash
go mod edit -dropreplace github.com/mattsp1290/eino-agui
go get github.com/mattsp1290/eino-agui@<tag-or-commit>
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
go test ./... -run Parity -count=1
```

`make check` runs:

- `gofmt`/`goimports` format checks
- `go vet ./...`
- `golangci-lint run ./...` through `go run`, pinned to `v2.12.2`
