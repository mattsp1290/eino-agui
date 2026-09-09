# AG-UI and Eino Upgrade

Status: Ready for implementation. Planning only; no implementation has occurred.

## Application context

```json
{
  "application_context": {
    "has_active_users": false,
    "backward_compatibility_required": false,
    "feature_flags": "not-applicable",
    "confirmation_digest": "935a4c000d321d51270123d0bf2910996c941664c125fb91a52906d5e2029e6a",
    "confirmed_at": "2026-09-09T04:11:34Z"
  }
}
```

The user confirmed that the library has no active users or external consumers and that backward compatibility is not required. The implementation can make a focused public API correction without a compatibility shim, migration period, or feature flags.

## Change classification

This is a dependency-boundary and protocol-bridge upgrade. It affects:

- module resolution in `go.mod` and `go.sum`;
- AG-UI message and tool conversion in `convert/` and `tools/`;
- typed SSE lifecycle and event emission in `emitter/`;
- streamed message identity and tool-call correlation in `stream/`;
- golden, unit, parity, example, architecture, and version documentation;
- release readiness for the next semver tag after `v0.1.1`.

It does not add an HTTP server, tool executor, checkpoint store, route policy, or Eino ADK runtime.

## Requested outcome

Upgrade the library to these exact dependency sources:

| Dependency | Required source | Resolved module form |
| --- | --- | --- |
| AG-UI Go SDK | [`mattsp1290/ag-ui@aaa75b54d572be8cd1d51c72e951273c5b893ed0`](https://github.com/mattsp1290/ag-ui/tree/aaa75b54d572be8cd1d51c72e951273c5b893ed0) | `github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260909025854-aaa75b54d572` replaced by `github.com/mattsp1290/ag-ui/sdks/community/go v0.0.0-20260909025854-aaa75b54d572` |
| Eino | [`cloudwego/eino@v0.9.19`](https://github.com/cloudwego/eino/tree/v0.9.19) | `github.com/cloudwego/eino v0.9.19` at commit `9d983b36a5112a1c233056b1a099825298fafb8f` |

The exact AG-UI commit is not present in the `ag-ui-protocol/ag-ui` repository. A direct query for the canonical module path fails with `unknown revision`; the fork module path resolves to the pseudo-version above. The `replace` directive is therefore required unless the same commit becomes reachable from the canonical repository before implementation. The implementer must not silently substitute a newer commit.

## Success criteria

1. `go list -m -json` proves the canonical AG-UI module uses the expected replacement, and `go mod download -json` proves the fork and Eino full origin hashes.
2. Every existing package builds against both targets with no local-checkout `replace` directive.
3. Reasoning streams retain `REASONING_MESSAGE_START`; the frame uses protocol role `reasoning`.
4. Live tool-call events carry one stable `parentMessageId`, and the returned wire transcript contains the assistant message that owns those tool calls.
5. The emitter can send every target-SDK event through its common error-handling path, including subagent lifecycle events and caller-built events with metadata or subagent attribution.
6. Eino `schema.TokenUsage` can be retained from complete or partial streams, converted to AG-UI token usage, and included on successful, interrupted, and non-transport-error run endings without fabricating absent counts.
7. AG-UI message and tool-call metadata, non-empty message subagent attribution, and tool-call encrypted continuity survive the supported in-memory AG-UI-to-Eino-to-AG-UI bridge path through namespaced Eino `Extra` values. Tool-definition metadata survives the one-way `types.Tool` to `schema.ToolInfo` binding path. Snapshot emission still removes all encrypted continuity values.
8. Golden fixtures and parity tests are recaptured or rewritten against the target AG-UI commit, and the full repository gates pass.

## Repository findings

- `go.mod` currently pins AG-UI pseudo-version `v0.0.0-20260624151131-d2049debabd9` and Eino `v0.8.13`.
- `docs/decisions/0001-eino-version-floor.md` deliberately chose `v0.8.13` as the old floor. The requested exact `v0.9.19` target supersedes that policy.
- A temporary module copy using the required pins compiles, but tests fail because `emitter.ReasoningMessageStart` sends role `assistant`; the target SDK requires `reasoning`.
- The target AG-UI Go SDK adds event/message/tool metadata, subagent lifecycle and attribution, `parentRunId`, tool-call `parentMessageId`, run token usage, capability models, stricter protocol validation, and corrected absent-versus-empty wire behavior.
- The target AG-UI Go example still uses Eino `model.ToolCallingChatModel` and `schema.Message`; it does not prove a production mapping for Eino's new `model.AgenticModel` or ADK runtime.
- The target example's stream loop preserves generated wire message IDs and assigns streamed tool calls to a stable assistant owner. The current library returns only the concatenated Eino message and emits tool calls without `parentMessageId`.
- Eino v0.9.19 retains all classic interfaces used here and adds `schema.AgenticMessage`, `model.AgenticModel`, richer tool results, tool search, and stream EOF hooks. Its classic `schema.Message.ResponseMeta.Usage` contains the five numeric counts needed by AG-UI token usage.
- Current `go test ./...` and `go build ./...` pass before the upgrade. The worktree was clean at planning start except for planning/beads artifacts created for this task.

## Feature disposition

| Target capability | Disposition for this library | Evidence or reason |
| --- | --- | --- |
| Corrected validation and wire presence | Adopt and test | Existing output changes under the target SDK; reasoning currently fails. |
| Event/message/tool-call metadata | Adopt bridge-safe round-trip pass-through and expose generic event emission | These are reusable protocol-envelope concerns. |
| Tool-definition metadata | Preserve on the existing one-way `ClientToolInfos` binding path | No reverse `schema.ToolInfo` to `types.Tool` API exists in this library. |
| Message `subagentRunId` | Preserve non-empty values in the namespaced Eino message envelope and restore on reverse conversion; normalize explicit empty to absence | Transcript grouping must survive conversion, but the SDK does not expose its internal explicit-empty presence bit. |
| Tool-call `encryptedValue` | Preserve in the namespaced Eino tool-call envelope, but scrub from all client snapshots | It supports continuity but is sensitive client-facing data. |
| Tool owner `parentMessageId` | Adopt in the stream result and emitter | It is required to preserve tool/message correlation across a run. |
| Run `parentRunId` and input echo | Expose through caller-built events via generic emission; document the pattern | Run orchestration owns the actual lineage and input. |
| Run token usage | Add Eino-to-AG-UI conversion and lifecycle emission support | Eino supplies compatible numeric usage in `ResponseMeta`. |
| Subagent lifecycle and attribution | Add typed lifecycle helpers plus generic attributed-event support | Emission is reusable; subagent scheduling remains app-owned. |
| Capability models | Document direct use of target SDK types; do not wrap them | This library does not own discovery endpoints or agent configuration. |
| Existing text, reasoning, state, activity, custom, interrupt, snapshot events | Retain and revalidate | These are already part of `emitter` and golden coverage. |
| Typed audio/video/document/binary conversion | Keep the current documented image-only conversion boundary | The target Go example still drops these inputs, and provider-safe conversion policy is not established. |
| Eino `AgenticModel`, ADK, MCP/server-tool blocks, and tool search | Defer | No current consumer or target example establishes an AG-UI mapping or lifecycle policy. |
| AG-UI HTTP/SSE client/server, persistence, tool execution, approvals | Keep app-owned | `docs/architecture/package-origins.md` already defines this boundary. |

## Key decisions

1. Use the exact fork pseudo-version and committed `replace`; do not use a filesystem replacement or float to fork `main`.
2. Raise the Eino floor to exactly v0.9.19 and replace Decision 0001's dual-version matrix. Supporting v0.8.x is not a goal.
3. Introduce a richer stream result so wire identity and tool ownership are first-class. Backward compatibility is not required, so do not preserve the old return shape through duplicate APIs.
4. Export one generic emitter entry point as the forward-compatible escape hatch, while retaining typed helpers for common lifecycle and subagent events. All events must share transport/encoding error classification.
5. Namespace protocol-only values placed in Eino `Extra`; never flatten arbitrary AG-UI metadata into provider-owned maps.

Rejected alternatives:

- A local filesystem `replace` would make builds checkout-specific and non-reproducible.
- Pinning the canonical module path directly to the fork commit does not resolve.
- Wrapping every new AG-UI SDK type would duplicate the SDK and make this library lag future additions.
- Adding an AgenticModel/ADK adapter now would invent orchestration semantics that neither the current library nor the target Go example exercises.

## Change model

```text
Before
AG-UI request -> convert -> classic Eino model stream
              -> emitter -> SSE frames
              -> *schema.Message only

After
AG-UI request -> metadata-aware convert -> Eino v0.9.19 classic model stream
              -> stream.Result { Assistant, WireMessages, ToolOwnerID, Usage, Partial }
              -> target-SDK-validated emitter -> correlated SSE frames
              -> optional mapped TokenUsage on run completion/error

Caller-built AG-UI event -> Emitter.Emit -> same validation/error/transport path
```

## Risks and gates

- Stop if the fork pseudo-version no longer resolves to the exact full hash. The owner is the repository maintainer; unblock by restoring that ref or selecting a new explicit source commit.
- Stop if target-SDK validation rejects a proposed wire sequence after the sequence tests use `events.ValidateSequence`. Fix the lifecycle design rather than weakening validation.
- Eino's integer usage fields do not preserve “reported zero” versus “not reported.” Map only positive counts and document this lossy boundary; do not fabricate pointers to zero. Accumulate usage separately from message concatenation so non-EOF exits retain observed counts.
- Eino `Extra` can contain provider data. Use a package-owned key and deep-enough copies to prevent collision and caller mutation.
- The `replace` is operational debt. Remove it only when the exact target content is reachable under the canonical module path and the resolved hash/content is verified.

There are no unresolved blocking decisions. There are no cross-repository requests: all reusable work belongs in this library, and adoption changes in other repositories are outside this request.

## Document map

- [01-dependency-baseline.md](01-dependency-baseline.md) — pin resolution, compatibility repairs, and stop/go checks.
- [02-protocol-bridge.md](02-protocol-bridge.md) — metadata, usage, emitter, and feature-surface changes.
- [03-stream-correlation.md](03-stream-correlation.md) — stream result, message identity, and tool-call lifecycle.
- [04-parity-docs-release.md](04-parity-docs-release.md) — golden provenance, coverage inventory, documentation, and release gates.
- [05-execution-handoff.md](05-execution-handoff.md) — dependency-ordered work packages and definition of done.
