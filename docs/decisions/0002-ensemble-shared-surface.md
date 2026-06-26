# Decision 0002: Ensemble Shared Surface

Date: 2026-06-26

## Decision

The ensemble Go backend is:

```text
/Users/punk1290/git/ensemble
github.com/mattsp1290/ensemble
HEAD a709ad8ed2e9d8962b73b228859433cc6554ee2c
```

It is a Go backend, but it is not currently an AG-UI SSE backend. It does not import:

```text
github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events
github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types
github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse
```

Therefore it does not contain duplicated copies of the reference app's AG-UI converter, emitter,
AG-UI stream tap, or AG-UI tool-binding functions. The first `eino-agui` extraction should treat
ensemble as an adjacent eino/tool-loop consumer, not as proof of the AG-UI public API shape.

The public AG-UI-facing API must still be finalized against `ag-ui-go-server-example` and should not
claim ensemble parity until a later ensemble migration task wires AG-UI transport into ensemble or
adds an adapter layer that consumes this library.

## Evidence

Repository discovery:

```bash
gh repo list mattsp1290 --limit 200 --json name,sshUrl,url,description
git clone git@github.com:mattsp1290/ensemble.git /Users/punk1290/git/ensemble
```

Module evidence:

```text
module github.com/mattsp1290/ensemble
go 1.26.3
require github.com/cloudwego/eino v0.8.13
```

Targeted validation:

```text
cd /Users/punk1290/git/ensemble
go test ./internal/worker/agent ./internal/obs ./internal/dispatcher
ok github.com/mattsp1290/ensemble/internal/worker/agent
ok github.com/mattsp1290/ensemble/internal/obs
ok github.com/mattsp1290/ensemble/internal/dispatcher

cd /Users/punk1290/git/ag-ui-go-server-example
go test ./internal/agent
ok github.com/mattsp1290/ag-ui-go-server-example/internal/agent
```

## Function-Level Comparison

| Planned unit | Reference app source | Ensemble status | API implication |
| --- | --- | --- | --- |
| Message/tool conversion | `internal/agent/convert.go`: `toEinoMessages`, `toEinoUserMessage`, `toEinoImagePart`, `toAGUIMessages`, `toEinoToolCalls`, `toAGUIToolCalls`, `messageText` | No AG-UI `types.Message`, `InputContent`, or `ToolCall` conversion code found. Ensemble constructs eino `schema.Message` directly in `internal/worker/agent/graph.go:renderMessages`. | The AG-UI converter surface is reference-app-derived for now. Ensemble does not validate AG-UI message conversion or vision gating. |
| Typed SSE emitter | `internal/agent/emitter.go`: `NewEmitter`, typed lifecycle/text/reasoning/tool/state/activity/custom methods, `isTransportError`, `scrubEncryptedValues` | No AG-UI `sse.SSEWriter`, AG-UI `events.Event`, `MessagesSnapshot`, or `StateSnapshot` emitter found. Ensemble uses internal `dispatcher.RunEvent` kinds in `internal/dispatcher/event.go`. | The AG-UI emitter package is not shaped by ensemble today. A future ensemble adapter would need to translate `dispatcher.RunEvent` to AG-UI events separately. |
| eino stream to AG-UI tap | `internal/agent/loop.go:streamTurn` consumes `model.Stream`, emits AG-UI text/reasoning/tool-call deltas, and returns `schema.ConcatMessages(chunks)`. | Ensemble uses `schema.ConcatMessageStream` inside `internal/worker/agent/graph.go` collect lambda and `internal/worker/agent/agent.go:invokeModelDirect`. It records output and OTel spans but does not emit AG-UI deltas. | Ensemble confirms the shared need for eino stream collection semantics, but not the AG-UI event-emission contract. Keep the stream tap narrow and document that callers must not separately emit duplicate tool proposals. |
| Tool-schema binding | `internal/agent/runconfig.go`: `clientToolInfos`, `toJSONSchema`, `classifyToolCalls` maps AG-UI client tools to eino `ToolInfo` and splits client/server calls. | No AG-UI `types.Tool` binding found. Ensemble has adjacent `internal/worker/agent/toolInfoSchemaJSON` and `internal/worker/agent/validator.go`, which convert `schema.ToolInfo` to JSON Schema and validate model-emitted `schema.ToolCall`s. | Do not extract ensemble's validator into the first AG-UI library. The AG-UI tool package should only cover reference app `types.Tool` to eino binding and client/server classification; a validator package would be separate scope. |
| Adjacent tool-call helpers | `internal/agent/loop.go`: `toolCallKey`, `validateToolCalls*`, `emitToolProposal`, `settlePendingToolCalls`. | Ensemble has `toolCallKey([]schema.ToolCall)`, `toolCallNames`, `synthesizeToolCallID`, `Validator`, and a ReAct tool loop in `internal/worker/agent/agent.go`. These are not AG-UI proposal/result helpers. | Keep proposal emission and app-specific settlement out of the core extraction unless later work explicitly designs a separate generic tool-loop package. |

## Shared Concepts From Ensemble

Ensemble still provides useful constraints for the library:

- eino v0.8.13 compatibility matters.
- `schema.ConcatMessageStream` / `schema.ConcatMessages` behavior is load-bearing for stream
  collection.
- Some providers omit tool-call IDs; ensemble synthesizes internal IDs for tool-message pairing.
- Tool validation and corrective messages are important, but ensemble's implementation is
  orchestrator-specific and evented through `dispatcher.RunEvent`, not AG-UI.
- Internal run/turn/tool events are semantically similar to AG-UI lifecycle and tool events, but the
  wire types and persistence requirements differ enough that they should not be collapsed in the
  first extraction.

## Scope Impact

This finding changes the precondition assumption: there is no second duplicated AG-UI implementation
to diff. The safe extraction path is:

1. Build the initial `eino-agui` public API from the reference app's AG-UI seam.
2. Keep the API narrow enough that ensemble can consume it later through an adapter rather than by
   rewriting its internal dispatcher event model.
3. File the ensemble consumption request after the library is green, describing the missing adapter
   work explicitly instead of claiming a direct import swap.
4. Revisit the public API if ensemble later adds AG-UI SSE transport or if its adapter needs a
   library primitive not covered by the reference-app extraction.

This does not block the reference-app extraction, but it does block claiming that the AG-UI public
API has been proven against two existing AG-UI consumers.
