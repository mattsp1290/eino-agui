# eino-agui

Reusable AG-UI helpers for CloudWeGo eino agents.

This module extracts the AG-UI/eino seam from
`github.com/mattsp1290/ag-ui-go-server-example` into small packages for:

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

Remove the replacement before depending on a published version. Until the first
semver tag is cut, use an explicit commit SHA or Go pseudo-version instead of
`@latest`:

```bash
go mod edit -dropreplace github.com/mattsp1290/eino-agui
go get github.com/mattsp1290/eino-agui@<tag-or-commit>
```

## Version Expectations

The module currently targets:

- Go `1.26.3`
- `github.com/cloudwego/eino v0.8.13`
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go`
  `v0.0.0-20260624151131-d2049debabd9`

Those versions are pinned in `go.mod`. Keep consuming apps on compatible eino
and AG-UI SDK versions when migrating the extracted seam.

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

See [examples/stream](examples/stream) for a minimal runnable example that
streams a deterministic eino model into AG-UI SSE events.

Run the example as a local smoke test:

```bash
go run ./examples/stream
```

It writes AG-UI SSE frames to stdout and the final concatenated assistant
content to stderr.

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
