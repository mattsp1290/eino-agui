# W1 — Public agentic contract and privacy boundary

Goal: define a closed public model that represents all requested Eino content and lifecycle semantics without exposing raw provider or ADK state. Prerequisite: the exact v0.9.19 source and pinned AG-UI SDK remain reproducibly downloadable.

## Evidence and existing patterns

- `internal/protocolmeta/protocolmeta.go` owns deep cloning for the existing package metadata envelope.
- `convert/convert.go` currently preserves only package-owned classic metadata but exposes encrypted tool-call values on reverse conversion.
- `emitter.scrubEncryptedValues` proves that emission must operate on a sanitized copy and must not mutate caller objects.
- Eino's `schema.ContentBlock` is a tagged struct with 20 optional variant pointers. Its constructors normally set one pointer, but callers can construct malformed or ambiguous values directly.
- A nested `schema.FunctionToolResultContentBlock` is another tagged union with five variants.
- Provider extensions mix public data with private continuation data. OpenAI response IDs and previous-response IDs, reasoning signatures, Claude encrypted citation indices, Gemini `SDKBlob`, arbitrary `Extension`, and arbitrary `Extra` cannot cross the bridge.

## Proposed change surface

- `convert/agentic_types.go` — **new** under existing `convert/`: exported `AgenticIdentityV1`, `AgentPathSegment`, `AgenticProjectionContext`, `AgenticBlockContext`, `ProjectionLimits`, `CandidateDigestV1`, `CommitReceiptV1`, `PublicAgenticMessage`, `PublicContentBlock`, `PublicToolDefinition`, nested public block payloads, `PublicProviderAnnotations`, `AgenticEnvelopeV1`, `AgenticEnvelopeKind`, and constants including `AgenticCustomEventName`.
- `convert/agentic_codec.go` — **new** under existing `convert/`: exported `ProjectAgenticMessage`, `DecodeAgenticEnvelope`, `ValidateAgenticEnvelope`, and internal exhaustive validation/clone helpers.
- `convert/agentic_codec_test.go` — **new** under existing `convert/`: table and fuzz coverage for outer/nested unions, allowlists, immutability and JSON decoding.
- `internal/protocolmeta/protocolmeta.go` — existing insertion point for reusable bounded cloning helpers only when they operate on already-public values. Do not add reflection-based cloning of arbitrary Eino `Extra`.
- `docs/decisions/0005-agentic-public-projection.md` — **new** under existing `docs/decisions/`: authoritative privacy and custom-envelope decision.

Proposed signatures are exact implementation targets unless W1 evidence forces a documented revision:

```go
const AgenticCustomEventName = "eino.agentic.v1"

func DefaultProjectionLimits() ProjectionLimits
func ProjectAgenticMessage(msg *schema.AgenticMessage, ctx AgenticProjectionContext) (*PublicAgenticMessage, error)
func ProjectionDigestV1(value *PublicAgenticMessage) (CandidateDigestV1, error)
func LifecycleDigestV1(value *AgenticEnvelopeV1) (CandidateDigestV1, error)
func DecodeAgenticEnvelope(value any) (*AgenticEnvelopeV1, error)
func ValidateAgenticEnvelope(value *AgenticEnvelopeV1) error
```

`AgenticProjectionContext` contains a base identity, positive limits and one `AgenticBlockContext` per final block ordinal. The base identity contains non-empty `SessionID`, `RunID`, `TurnID`, `MessageID`, `AttemptID` and ordered `AgentPath`. `ThreadID` is the AG-UI alias of the host session identity and must match rather than creating a second namespace. Each block context contains its caller-supplied `BlockID`; server-tool blocks also require a stable `ProviderServerID`; approval responses require an `ExpectedApprovalRequestID`; and optional host-validated `ApprovalInterruptCorrelation` keeps the native approval ID and ADK target ID/address in separate fields. The converter never infers these values from names, prior global state or each other.

`AgenticEnvelopeV1` contains `version: 1`, one closed `kind` discriminator, the common identity tuple, and exactly one matching payload. Initial kinds are `content_block`, `provider_annotations`, `run_started`, `run_finished`, `run_error`, `turn_started`, `turn_finished`, `attempt_replaced`, `subagent_started`, `subagent_finished`, `subagent_error`, `paused`, `resumed`, and `cancelled`. Unknown versions/kinds, a missing selected payload, extra selected payloads, invalid identity combinations and unknown JSON fields fail deterministically. `AgenticIdentityV1` is the only value allowed at native event `metadata["eino.agentic.v1"]`; every start/content/end event in one native block sequence carries an identical identity, and the matching committed content-block supplement is authoritative after commit.

Every returned projection or lifecycle candidate includes the digest the host stores with its revision. `ProjectionDigestV1` and `LifecycleDigestV1` are the public constructors for the same values. Hash lowercase SHA-256 over `eino-agentic-v1\0projection\0<canonical-json>` or `eino-agentic-v1\0lifecycle\0<kind>\0<canonical-json>`. Canonical JSON follows RFC 8785 and includes schema version, full durable identity and all public payload fields but excludes the receipt and transport-generated event IDs/timestamps. Lifecycle hashing is kind-specific, so a receipt cannot authorize a different fact. Publish fixed digest vectors for independent consumers and reject altered run, kind, identity, payload, version or digest.

`ProjectionLimits` is shared by conversion, model streaming and ADK projection. Proposed defaults are 8 MiB encoded public message, 1 MiB encoded block, 1,024 blocks/message, 1,024 tool definitions, 4,096 annotations/grounding entries, 256 interrupt targets, 64 agent-path segments, JSON depth 64 and 4,096 JSON object/array entries per arbitrary server value. Validate positive limits at construction. Count encoded base64 length without decoding it; walk arbitrary JSON-compatible values with cycle detection and overflow-safe counters before allocating clones or JSON buffers.

## Content mapping

The table defines the lossless public projection and the AG-UI wire view. “Custom” always means one `CUSTOM` event with name `eino.agentic.v1`; it does not authorize additional ad hoc event names.

| Eino v0.9.19 kind | Public payload | AG-UI projection |
| --- | --- | --- |
| `reasoning` | visible text only | Native reasoning event sequence. Strip `Signature`, raw provider reasoning and arbitrary extensions. |
| `user_input_text` | text | Native user `types.Message` content for snapshots/replay plus a committed custom identity/ordinal supplement. |
| `user_input_image` | URL or base64, MIME, detail | Native `types.InputContent` image plus custom identity/ordinal. Validate exactly one source. |
| `user_input_audio` | URL or base64 and MIME | Native audio input plus custom identity/ordinal. |
| `user_input_video` | URL or base64 and MIME | Native video input plus custom identity/ordinal. |
| `user_input_file` | URL or base64, name and MIME | Native document input plus custom identity/ordinal. |
| `tool_search_result` | call ID, name and ordered closed tool definitions | Custom. Preserve `ToolInfo` name/description, parameter representation (`none`, structured `params`, or `json_schema`) and nil-versus-present-empty semantics; exclude `ToolInfo.Extra`. Distinguish deferred discovery from tool execution. |
| `assistant_gen_text` | text plus allowed refusal/citations | Native assistant text sequence; custom provider annotations keyed to the same block when present. |
| `assistant_gen_image` | URL or base64 and MIME | Custom content block. |
| `assistant_gen_audio` | URL or base64 and MIME | Custom content block. |
| `assistant_gen_video` | URL or base64 and MIME | Custom content block. |
| `function_tool_call` | call ID, name and JSON argument text | Native `TOOL_CALL_*`; custom identity/ordinal only when mixed ordering or agent/attempt identity is not expressible natively. |
| `function_tool_result` | call ID, name and ordered nested text/image/audio/video/file parts | Native `TOOL_CALL_RESULT` only for the exact text-only representable view; custom is authoritative for structured or mixed results. Never stringify media. |
| `server_tool_call` | caller-supplied provider-server ID, call ID, name and bounded JSON-compatible arguments | Custom. Eino does not supply the provider-server ID, so reject missing block context. Mark `executionOwner: provider`; never classify as a local function tool. |
| `server_tool_result` | caller-supplied provider-server ID, call ID, name and bounded JSON-compatible content | Custom. Eino does not supply the provider-server ID. Preserve error/status only through defined fields. |
| `mcp_tool_call` | server label, approval request ID, call ID, name and argument text | Custom. Mark `executionOwner: provider_mcp`. |
| `mcp_tool_result` | server label, call ID, name, content and typed code/message error | Custom. |
| `mcp_list_tools_result` | server label, ordered tool definitions and typed error | Custom. |
| `mcp_tool_approval_request` | native approval ID, server label, name and arguments | Custom. It is not an ADK interrupt by itself. |
| `mcp_tool_approval_response` | native approval request ID, approve and reason | Custom. Require exact equality with the block context's expected approval request ID. |

For standard-native plus custom projections, native deltas are the transient rendering view and the post-commit custom envelope is the durable semantic supplement. Every native fragment carries the closed identity metadata even for a single reasoning, text or function-call block with no annotations. Consumers merge only when the full identity tuple matches. They must not append the custom payload as a second logical block.

`PublicToolDefinition` uses an explicit `paramsKind`. Obtain the upstream distinction through `ToolInfo.MarshalJSON` and a private strict mirror of `has_params_one_of`, `params` and `json_schema`; do not access unexported Eino fields or normalize both forms through `ToJSONSchema`. `none` means nil `ParamsOneOf`; `params` preserves a present empty map; `json_schema` preserves its schema. Reject nil tool entries, duplicate names, both representations, invalid nested parameter trees and limit overflow.

## Provider annotation allowlist

Project only explicitly public fields:

- OpenAI assistant text: refusal reason and file/URL/container/file-path annotations attached to the owning `assistant_gen_text` block. Require the annotation type to select exactly one matching pointer. Validate nonnegative indices and URL citation byte ranges within that block's UTF-8 text. Strip response ID, previous response ID and all raw continuation state.
- Claude assistant text: character, page, content-block and web citations attached to the owning `assistant_gen_text` block. Require exactly one matching citation variant, nonnegative source indices and ordered ranges. These are source-document spans, so do not validate them against output-text length. Strip `EncryptedIndex`.
- Gemini response metadata: finish reason, web grounding title/domain/URI, supports/segments, confidence scores, web-search queries and rendered search content. Keep chunks and search-entry data at message level. Map each support's zero-based `Segment.PartIndex` to the final `ContentBlocks` ordinal, require an `assistant_gen_text` target, validate start/end as UTF-8 byte offsets within that block, and validate every `GroundingChunkIndex` against the message-level chunk list. Reject absent or ambiguous associations. Strip `SDKBlob`.
- Public terminal status: bounded provider error code/message, incomplete reason and documented finish/stop details where they explain the visible result. Do not include provider response IDs, service metadata, raw reasoning control or arbitrary extension values.
- Token usage: reuse `convert.ToAGUITokenUsage` semantics for positive values and add an agentic wrapper over `AgenticResponseMeta.TokenUsage`.

Reject non-JSON-compatible values on public `any` fields such as server-tool arguments/results. Do not silently stringify them. Unknown `Extra` and extension values remain absent from output. Validation scans emitted JSON/SSE, not only Go structs.

## Invariants and error behavior

- Projection validates shared limits and required block context before cloning, marshaling or emitting, and never mutates input pointers, slices, maps, schemas or extension objects.
- Outer and nested unions require a known discriminator and exactly one matching non-nil field. A mismatched extra variant is an error even if the selected variant is valid.
- Final block order is slice order. Streaming block order is final `StreamingMeta.Index`, with first-seen index used only as a transient scheduling aid.
- Message role and block kinds must be compatible with Eino's agentic grammar. Function results remain user-role content and are not rewritten to a classic tool role.
- Durable IDs must be non-empty where required and unique within their owner. The bridge does not call AG-UI ID generators for durable message/block/attempt/call identity.
- Approval IDs and ADK interrupt addresses occupy separate fields and never fall back to one another.
- A `CommitReceiptV1` contains non-empty host revision, candidate domain/kind, full identity and candidate digest. Committed-emission helpers recompute/compare every binding but do not claim to verify the host database itself.
- Public codec errors are typed with message/block ordinal and field path, but error text must not echo payload bodies.

## Verification and acceptance

- Table-test all 20 variants and all five nested function-result variants through project → JSON → strict decode.
- Test mixed ordered blocks and merge-by-identity without logical duplication.
- Test single native-only-shaped reasoning, text and function-call blocks recover the full identity tuple from every SDK-decoded fragment and the committed supplement.
- Test every malformed union shape: nil block, empty/unknown discriminator, nil selected pointer, two selected pointers, mismatched pointer, unknown JSON member and invalid nested result.
- Seed every forbidden field with a unique sentinel and assert absence in projected JSON, errors, native AG-UI events and the custom envelope.
- Mutate every returned nested slice/map/pointer and prove the original Eino input remains unchanged; mutate the input after projection and prove output remains unchanged.
- Fuzz `DecodeAgenticEnvelope` and union validation for deterministic errors and no panic.
- Round-trip tool definitions in structured params, JSON schema, nil and present-empty forms; cover nested schemas, duplicate/nil entries and `Extra` exclusion.
- Test exact and one-over limits, deeply nested and cyclic arbitrary server values, overflow-safe counters and rejection before clone/encode allocation.
- Publish fixed projection and lifecycle digest vectors. Verify a separately implemented test canonicalizer matches them across map order and numeric/string edge cases; reject cross-kind, cross-run, altered-field and replayed receipts.
- Test multiple assistant-text blocks with valid and invalid OpenAI/Claude citations and Gemini grounding. Reconstruct the referenced text/source span and reject wrong variants, negative/reversed/out-of-range indices, invalid UTF-8 byte offsets and ambiguous part association.
- Run `go test ./convert` and `go test -race ./convert`.

Exclusions: no generic provider-extension registry, no public raw `Extra`, no asset retrieval and no host persistence model.
