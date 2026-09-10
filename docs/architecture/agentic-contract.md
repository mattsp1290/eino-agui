# Agentic runtime bridge contract

The agentic path is a projection and observation boundary for Eino v0.9.19.
It does not run tools, persist turns, authorize approvals, store checkpoints,
or decide when a run is complete.

## Public entry points

The primary exported signatures are:

```go
func convert.DefaultProjectionLimits() convert.ProjectionLimits
func convert.ToAgenticProjection(*schema.AgenticMessage, convert.AgenticProjectionContext) (*convert.AgenticProjection, error)
func convert.ProjectionDigestV1(*convert.PublicAgenticMessage) (convert.CandidateDigestV1, error)
func convert.LifecycleDigestV1(*convert.AgenticEnvelopeV1) (convert.CandidateDigestV1, error)
func convert.DecodeAgenticEnvelope(any) (*convert.AgenticEnvelopeV1, error)

func emitter.NewObserverEmitter(context.Context, *bufio.Writer, *sse.SSEWriter) *emitter.Emitter
func emitter.NewObserverSink(*emitter.Emitter) *emitter.ObserverSink

func stream.StreamAgenticTurn(context.Context, model.AgenticModel, []*schema.AgenticMessage,
    stream.AgenticStreamIdentity, stream.BlockContextResolver, ...stream.AgenticOption) (*stream.AgenticResult, error)
func stream.NewAgentEventSource(*adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]],
    func(error), func(context.Context) error) (stream.AgentEventSource, error)
func stream.DrainAgenticEvents(context.Context, stream.AgentEventSource,
    stream.AgentEventIdentityResolver, ...stream.AgentEventOption) (*stream.AgentEventResult, error)
func stream.WithAgentEventCancellationCandidate(stream.AgenticStreamIdentity,
    convert.CancelledV1) stream.AgentEventOption

// package convert
type ToolExecutionOwner string
const (
    ToolExecutionOwnerProvider ToolExecutionOwner = "provider"
    ToolExecutionOwnerProviderMCP ToolExecutionOwner = "provider_mcp"
)

type CancellationModeV1 string
const (
    CancellationModeImmediate CancellationModeV1 = "immediate"
    CancellationModeAfterChatModel CancellationModeV1 = "after_chat_model"
    CancellationModeAfterToolCalls CancellationModeV1 = "after_tool_calls"
    CancellationModeAfterChatOrToolCalls CancellationModeV1 = "after_chat_model_or_tool_calls"
)

type CancellationClassificationV1 string
const (
    CancellationClassImmediate CancellationClassificationV1 = "immediate"
    CancellationClassSafePoint CancellationClassificationV1 = "safe_point"
    CancellationClassEscalated CancellationClassificationV1 = "escalated"
    CancellationClassTimeout CancellationClassificationV1 = "timeout"
)
```

Conversion starts with:

```go
projection, err := convert.ToAgenticProjection(message, convert.AgenticProjectionContext{
    Identity: identity,
    Blocks: blockContexts,
    Limits: convert.DefaultProjectionLimits(),
})
```

`Identity` contains host-assigned session/run/turn/message/attempt and ordered
agent-path IDs. There is exactly one block context per final block, each with a
host-assigned block ID. Server-tool records additionally require a provider
server ID. Approval responses require the expected native MCP request ID.
Native MCP request IDs and ADK interrupt IDs/addresses are separate fields;
the bridge accepts a correlation only when the host supplies the complete pair.

One model request is observed with an observer-only emitter adapter. The
emitter context belongs to that observer connection; `ctx` remains the separate
host execution context:

```go
emit := emitter.NewObserverEmitter(observerCtx, writer, sseWriter)
sink := emitter.NewObserverSink(emit)
result, err := stream.StreamAgenticTurn(
    ctx, agenticModel, messages, stream.AgenticStreamIdentity{...}, resolver,
    stream.WithTransientSink(sink),
)
```

The result is an uncommitted candidate. A sink failure detaches that observer
and does not cancel the model. Context cancellation remains the host execution
cancellation signal. The helper never retries and never emits a turn or run
completion.

Typed ADK iterators must be wrapped with an abort hook that unblocks `Next` and
a wait hook that joins the producer:

```go
source, err := stream.NewAgentEventSource(iterator, abort, wait)
result, err := stream.DrainAgenticEvents(
    ctx, source, identityResolver,
    stream.WithAgentEventCancellationCandidate(ids, cancellationFact),
)
```

Complete and exclusive streamed message variants become candidate projections.
For business interrupts, the resolver supplies a durable pause ID alongside the
public ADK target IDs/addresses and any separately validated MCP approval
correlation. Each returned `InterruptCandidate` carries that pause plus the exact
resolved turn/attempt identity needed for its commit receipt. Cancellation
candidates carry the same identity scope. The optional configured cancellation
candidate records host policy if the execution context is cancelled; it never
causes cancellation. Transfer, exit, and break-loop remain host control
observations carrying their exact resolved identity; they do not imply a
lifecycle terminal. Arbitrary customized output/action is rejected.

Cancellation modes and classifications form a closed v1 vocabulary. Immediate
cancellation requires immediate requested and observed modes. A safe-point
observation repeats one non-immediate requested mode exactly. Escalated and
timeout outcomes retain the requested safe-point mode and report immediate as
the observed mode. Unknown or inconsistent combinations are rejected rather
than projected as an `unknown` durable fact.

## Commit and delivery

The host stores the candidate and its digest, then supplies a
`convert.CommitReceiptV1` binding a non-empty revision, candidate domain or
lifecycle kind, full identity, and digest. The emitter recomputes that binding
before writing any authoritative fact.

```go
emit := emitter.NewObserverEmitter(ctx, writer, sseWriter)
ok := emit.EmitCommittedProjection(projection, receipt,
    emitter.DeliveryModeLiveContinuation)
```

Delivery modes are:

- `live_continuation`: emit only committed custom supplements after transient
  native chunks;
- `committed_only`: emit the complete native view and supplements once; and
- `replay`: reconstruct the same complete committed view in a fresh client.

Run, turn, attempt, pause/resume, cancellation, and subagent helpers also
require lifecycle receipts. A run finish/error additionally requires
`loopSettled=true`. Turn completion and pause never imply run completion.
Paused facts carry a durable `pauseId`; a resume on the same emitter requires
that committed pause and either its exact full target list or an ordered strict
subset. Pause IDs cannot be reused within a run. A correlated approval must be
repeated exactly when its target is resumed, and cannot be inferred or swapped
to another target. New turn and attempt IDs must differ from the paused identity.

A receipt can authorize one successful logical emission per emitter. Reusing it
on the same connection is rejected before bytes; a fresh replay emitter has its
own receipt-consumption state. If a projection has public response metadata, a
final `response_meta` custom envelope preserves token usage, bounded provider
terminal details, and Gemini grounding after the ordered content blocks.

## Content mapping

The closed `eino.agentic.v1` projection maps all 20 Eino kinds:

| Eino kind | Public/AG-UI view |
| --- | --- |
| `reasoning` | visible text; native reasoning events |
| `user_input_text` | text and identity supplement |
| `user_input_image` | exactly one URL/base64 image source |
| `user_input_audio` | exactly one URL/base64 audio source |
| `user_input_video` | exactly one URL/base64 video source |
| `user_input_file` | exactly one URL/base64 document source plus name |
| `tool_search_result` | call identity and closed ordered tool definitions |
| `assistant_gen_text` | native text plus allowlisted annotations |
| `assistant_gen_image` | closed generated-image block |
| `assistant_gen_audio` | closed generated-audio block |
| `assistant_gen_video` | closed generated-video block |
| `function_tool_call` | native tool-call events plus durable identity |
| `function_tool_result` | native only for one text part; structured custom otherwise |
| `server_tool_call` | provider-owned closed arguments, provider-server ID, and `executionOwner: provider` |
| `server_tool_result` | provider-owned closed result, provider-server ID, and `executionOwner: provider` |
| `mcp_tool_call` | provider-MCP call record with `executionOwner: provider_mcp` |
| `mcp_tool_result` | provider-MCP result and typed error with `executionOwner: provider_mcp` |
| `mcp_list_tools_result` | ordered closed MCP tool definitions/error |
| `mcp_tool_approval_request` | native MCP approval request, not an ADK interrupt |
| `mcp_tool_approval_response` | response requiring the expected native request ID |

Function results preserve the five nested `text`, `image`, `audio`, `video`,
and `file` variants. Tool definitions preserve `none`, structured `params`, and
`json_schema`, including nil versus present-empty structured params.

## Privacy and limits

Default limits are 8 MiB/message, 1 MiB/block, 1,024 blocks, 1,024 tool
definitions, 1,048,576 stream chunks, 4,096 annotation/grounding entries, 256
interrupt targets, 64 agent-path segments, JSON depth 64, and 4,096 JSON
entries. Callers may pass a different all-positive `ProjectionLimits` value.

For user-role records, `AgenticProjection.NativeMessage` contains the native
AG-UI `types.Message`/`types.InputContent` view. Hosts combine those committed
records into a complete `MESSAGES_SNAPSHOT`; the bridge does not emit a partial
one-message snapshot that could overwrite unrelated transcript history.

Raw `Extra`, arbitrary provider extensions, encrypted reasoning signatures,
provider response/continuation IDs, Claude encrypted citation indices, Gemini
SDK blobs, checkpoint bytes, and arbitrary interrupt info never enter the
public projection. Server-tool `any` values must be finite, acyclic,
JSON-compatible data. Digests use RFC 8785 canonical JSON and domain-separated
SHA-256.
