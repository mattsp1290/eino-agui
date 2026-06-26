#!/bin/bash
# Project: eino-agui — extract the shared AG-UI + eino integration seam into a
#          canonical Go library (github.com/mattsp1290/eino-agui).
# Generated: 2026-06-26
#
# This script builds a dependency-aware Beads task graph for multi-agent execution.
# One parent epic; every task is parented to it via --parent "$EPIC".
# `bd dep add CHILD PARENT` edges encode real ordering between TASK beads only —
# never to or from the epic (the epic is an organizational rollup, not work).

set -euo pipefail

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating project beads..."

# ========================================
# Parent epic — every task below is parented to it (--parent "$EPIC").
# Marked in_progress immediately so status-based exclusion keeps it out of `bd ready`.
# ========================================

EPIC=$(bd create "Epic: eino-agui — extract shared AG-UI+eino seam into a library" -t epic -p 0 --silent)
bd update "$EPIC" --status in_progress   # rollup, not dispatchable work — keep it out of `bd ready`

# ========================================
# Phase 0: BLOCKING preconditions — resolve unknowns BEFORE any extraction.
# These gate the whole graph: an undecided eino floor, an unverified SDK pin, or an
# un-diffed second consumer can each invalidate the public API after it's built.
# ========================================

PRE_EINO=$(bd create "Audit eino 0.9.2-only symbols vs 0.8.13; decide the version floor" \
  -d "Enumerate the v0.9.x-only symbols the seam touches: schema.MessageInputPart, schema.MessageInputImage, UserInputMultiContent, schema.ChatMessagePartTypeImageURL, chunk.ReasoningContent, and Extra-preserving schema.ConcatMessages. Confirm which actually exist in v0.8.13. Determine whether the ensemble backend / eino-providers / eino-tools (on v0.8.13) can absorb a forced MVS bump to 0.9.2 — a consumer importing this lib is force-bumped regardless of its own pin. Output: the pinned eino floor + written rationale. If 0.8.13 lacks required symbols AND ensemble cannot bump, record the finding and renegotiate scope (drop 0.9.2-forcing features or accept ensemble-first migration)." \
  -p 0 --parent "$EPIC" --silent)

PRE_ENSEMBLE=$(bd create "Locate ensemble Go backend; extract+diff its duplicated eino/AG-UI funcs vs the reference app" \
  -d "Confirming a path is NOT validating API fit. The ensemble Go backend is NOT checked out under ~/git (~/git/ensemble-ui is the Flutter frontend). Find/check out the real repo, extract its currently-duplicated converters/emitter/stream-tap/tool-binding, and diff them against ag-ui-go-server-example's internal/agent versions. Derive the common surface from BOTH consumers. Record any divergence; if the abstraction does not fit both, renegotiate scope before extraction. Output: confirmed ensemble repo path/name + a per-function shared-surface diff report." \
  -p 0 --parent "$EPIC" --silent)

PRE_SDK=$(bd create "Pin a published AG-UI Go SDK version; verify public == local fork; align both consumers" \
  -d "The reference app consumes the SDK via a go.mod replace to a local untagged fork at ~/git/ag-ui (pseudo-version v0.0.0-...). A published library CANNOT ship that replace. (a) Pin a real published commit/tag of github.com/ag-ui-protocol/ag-ui/sdks/community/go. (b) Verify the public SDK matches the local fork the seam relies on — confirm no unpushed patches the converters/emitter/isTransportError depend on. (c) Pin the SAME SDK version across both consumers (SDK version skew silently breaks isTransportError disconnect detection, which string-matches SDK-internal error prefixes). Output: pinned SDK version + verification notes." \
  -p 0 --parent "$EPIC" --silent)

# ========================================
# Phase 1: Project setup & infrastructure
# Files: go.mod, go.sum, .golangci.yml, .github/workflows/**
# ========================================

SETUP_MOD=$(bd create "Initialize Go module github.com/mattsp1290/eino-agui (Go 1.26)" \
  -d "Library-only: NO main package, NO HTTP framework dependency (gofiber stays in the apps). Add core deps at the pinned versions: github.com/cloudwego/eino (floor from PRE_EINO), github.com/ag-ui-protocol/ag-ui/sdks/community/go (version from PRE_SDK), github.com/eino-contrib/jsonschema. Lay out sub-packages: convert/, emitter/, stream/, tools/." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$SETUP_MOD" "$PRE_EINO"
bd dep add "$SETUP_MOD" "$PRE_SDK"

SETUP_LINT=$(bd create "Configure golangci-lint, gofmt/goimports, go vet" \
  -d "Add .golangci.yml with a sensible enabled-linter set for a public library. Wire gofmt/goimports formatting and go vet. Document the local invocation in the README later." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$SETUP_LINT" "$SETUP_MOD"

SETUP_CI=$(bd create "Add GitHub Actions CI: build + vet + lint + test" \
  -d "Workflow runs go build ./..., go vet ./..., golangci-lint run, and go test ./... on Go 1.26. Gate PRs on green." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$SETUP_CI" "$SETUP_LINT"

# ========================================
# Phase 2: Deterministic golden-parity harness — lock UNIT behavior BEFORE extraction.
# Parity = normalized/ID-masked structural equivalence at the UNIT level, NOT byte-for-byte,
# NOT a full Run() replay.
# Files: internal/testharness/**, testdata/golden/**
# ========================================

HARNESS_ID=$(bd create "Add deterministic ID-generator seam for tests (per-Emitter option or guarded global)" \
  -d "aguievents.GenerateMessageID() is non-deterministic, and the only current override seam events.SetDefaultIDGenerator is PROCESS-GLOBAL. Provide a deterministic, monotonically-stable ID generator for fixtures and design for test isolation/concurrency — prefer a per-Emitter ID-generator option over mutating global state; if the global must be used, serialize/guard it." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$HARNESS_ID" "$SETUP_MOD"

HARNESS_FAKE=$(bd create "Build fake eino StreamReader/ToolCallingChatModel with fixed chunks + stable tc.Index pointers" \
  -d "Emit a fixed, replayable chunk sequence covering text, reasoning (incl. encrypted), and tool calls. The stream tap keys tool calls on *tc.Index, so the fake MUST hand out STABLE tc.Index pointers across chunks for the same call." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$HARNESS_FAKE" "$SETUP_MOD"

HARNESS_SINK=$(bd create "Build byte-capturing SSEWriter sink for fixture capture and assertions" \
  -d "A test sink that wraps/imitates AG-UI sse.SSEWriter over a *bufio.Writer and captures emitted frames as bytes for golden comparison. Pairs with the ID generator and normalization layer." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$HARNESS_SINK" "$SETUP_MOD"

GOLDEN=$(bd create "Capture normalized (ID-masked) golden SSE fixtures for the four extracted units" \
  -d "Parity = normalized structural equivalence, NOT byte-for-byte. Build a normalizer that masks runtime-minted IDs and timestamps. Capture golden frames from the CURRENT implementation of convert/emitter/streamTurn/tool-binding funcs (in ag-ui-go-server-example) via the byte-capturing sink + deterministic IDs + fake stream. Lock these behaviors: reasoning incl. encrypted-reasoning scrubbing in MESSAGES_SNAPSHOT, tool-call buffered-START (TOOL_CALL_START only once id+name both known), block-ordering (close text/reasoning before tool-call events), parameterized vision gating." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$GOLDEN" "$HARNESS_ID"
bd dep add "$GOLDEN" "$HARNESS_FAKE"
bd dep add "$GOLDEN" "$HARNESS_SINK"

# ========================================
# Phase 3: convert package — bidirectional eino schema.Message <-> AG-UI types.Message.
# Extract: toEinoMessages, toEinoUserMessage, toEinoImagePart, toAGUIMessages,
#          toEinoToolCalls, toAGUIToolCalls, messageText.
# Files: convert/**, convert/*_test.go
# ========================================

CONVERT_CORE=$(bd create "convert: extract bidirectional message conversion (toEinoMessages/toAGUIMessages/messageText)" \
  -d "Port toEinoMessages, toEinoUserMessage, toAGUIMessages, and messageText into the convert package with doc comments. Keep app-specific routing OUT. Surface must be derived from BOTH consumers (see PRE_ENSEMBLE diff)." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$CONVERT_CORE" "$GOLDEN"
bd dep add "$CONVERT_CORE" "$PRE_ENSEMBLE"

CONVERT_VISION=$(bd create "convert: parameterize vision gating as an injected capability predicate" \
  -d "Today supportsVision() literally returns provider == \"openai\". Replace with an injected predicate / capability option (default: a documented provider allowlist) so a second consumer with different provider names neither silently loses nor wrongly enables images. Must NOT hardcode openai." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$CONVERT_VISION" "$CONVERT_CORE"

CONVERT_MULTIMODAL=$(bd create "convert: extract multimodal/vision image-part handling (toEinoImagePart)" \
  -d "Port toEinoImagePart and the multimodal/vision content path, gated by the injected vision predicate from CONVERT_VISION. Uses v0.9.x symbols schema.MessageInputPart/MessageInputImage/UserInputMultiContent/ChatMessagePartTypeImageURL — confirm availability per PRE_EINO." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$CONVERT_MULTIMODAL" "$CONVERT_VISION"

CONVERT_TOOLCALLS=$(bd create "convert: extract tool-call conversion (toEinoToolCalls/toAGUIToolCalls)" \
  -d "Port bidirectional tool-call conversion. Validate tool-call ids; do not panic on malformed AG-UI input." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$CONVERT_TOOLCALLS" "$CONVERT_CORE"

CONVERT_TESTS=$(bd create "convert: table-driven + golden tests (vision allow/deny, multimodal, tool calls)" \
  -d "Table-driven unit tests for every converter, asserting against normalized golden fixtures. Cover vision allowlisted vs denied provider, multimodal image parts, tool-call round-trips, and malformed-input robustness." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$CONVERT_TESTS" "$CONVERT_MULTIMODAL"
bd dep add "$CONVERT_TESTS" "$CONVERT_TOOLCALLS"
bd dep add "$CONVERT_TESTS" "$GOLDEN"

# ========================================
# Phase 4: emitter package — typed wrapper over AG-UI sse.SSEWriter.
# Constructor is EXPLICIT: *bufio.Writer + *sse.SSEWriter + cancel context.CancelFunc.
# NOT a bare io.Writer.
# Files: emitter/**, emitter/*_test.go
# ========================================

EMITTER_CORE=$(bd create "emitter: extract NewEmitter with explicit bufio+SSEWriter+cancel constructor" \
  -d "Reproduce the exact current NewEmitter shape: binds *bufio.Writer + *sse.SSEWriter + a cancel context.CancelFunc (disconnect design is shaped by fasthttp, which only signals disconnect via a failed SSE write). Document this signature explicitly. Do NOT offer a generic io.Writer constructor — callers wrap their own writer." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$EMITTER_CORE" "$GOLDEN"
bd dep add "$EMITTER_CORE" "$PRE_SDK"

EMITTER_EVENTS=$(bd create "emitter: typed methods for every event family (lifecycle/text/reasoning/tool/state/activity/steps/custom)" \
  -d "Expose typed methods for run lifecycle, text, reasoning (incl. encrypted), tool calls with buffered-START semantics (emit TOOL_CALL_START only once id+name both known), state snapshot/delta, messages snapshot, activity, steps, and custom events. Preserve block-ordering (close text/reasoning before tool-call events)." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$EMITTER_EVENTS" "$EMITTER_CORE"

EMITTER_REASONING=$(bd create "emitter: encrypted-reasoning scrubbing in MESSAGES_SNAPSHOT (no leakage to clients)" \
  -d "Ensure encrypted reasoning content is scrubbed and never leaks to clients via snapshots. Lock this behavior against the golden fixture." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$EMITTER_REASONING" "$EMITTER_EVENTS"

EMITTER_ERRORS=$(bd create "emitter: transport-vs-encoding error separation + client-disconnect detection" \
  -d "A client disconnect (transport error) cancels the run context via the cancel func; an encoding/validation error drops the single event but keeps the stream alive. Implement isTransportError classification and the cancel-on-disconnect path." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$EMITTER_ERRORS" "$EMITTER_CORE"

EMITTER_TRANSPORT_TEST=$(bd create "emitter: isTransportError regression test that fails if SDK error-string prefixes change" \
  -d "isTransportError classifies disconnects by string-matching the SDK's internal wrapper prefixes (\"SSE write failed\", \"SSE flush failed\"). Add a regression test that FAILS if those prefixes change in the pinned SDK. Separately evaluate upstreaming a typed/sentinel error to the AG-UI SDK rather than carrying brittle string matching into a shared library — record the recommendation." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$EMITTER_TRANSPORT_TEST" "$EMITTER_ERRORS"
bd dep add "$EMITTER_TRANSPORT_TEST" "$PRE_SDK"

EMITTER_TESTS=$(bd create "emitter: golden tests for event families, buffered-START, block-ordering, reasoning scrub" \
  -d "Table-driven + golden tests asserting normalized event sequences for each family, buffered TOOL_CALL_START semantics, text/reasoning-before-tool block-ordering, and encrypted-reasoning scrubbing. Verify encoding-error drops event but keeps stream alive." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$EMITTER_TESTS" "$EMITTER_EVENTS"
bd dep add "$EMITTER_TESTS" "$EMITTER_REASONING"
bd dep add "$EMITTER_TESTS" "$EMITTER_ERRORS"

# ========================================
# Phase 5: stream package — reusable streamTurn() equivalent (eino StreamReader -> AG-UI tap).
# Cross-cut contract: callers MUST NOT also emit a tool proposal for the same calls.
# Files: stream/**, stream/*_test.go
# ========================================

STREAM_ASSIGN=$(bd create "stream: decide extracted-vs-stays for emitToolProposal/validateToolCalls*/settlePendingToolCalls" \
  -d "Explicitly assign each adjacent helper to EXTRACTED (into the library) or STAYS-IN-APP: emitToolProposal, validateToolCalls, validateToolCallsQuiet, settlePendingToolCalls. Leaving these unassigned re-introduces the duplication the library exists to remove. Ground the decision in the PRE_ENSEMBLE diff. Output: a written assignment table with rationale." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$STREAM_ASSIGN" "$PRE_ENSEMBLE"
bd dep add "$STREAM_ASSIGN" "$GOLDEN"

STREAM_TAP=$(bd create "stream: extract streamTurn() tap (StreamReader -> live events, returns concatenated message)" \
  -d "Consume an eino StreamReader; emit text/reasoning/tool-call events live, keying tool calls on toolCallKey -> *tc.Index; return the concatenated schema.Message (Extra-preserving ConcatMessages). Expose WITHOUT the app-specific agent-loop business logic. Document the cross-cut contract: callers MUST NOT also emit a tool proposal for the same calls. Implement only the helpers marked EXTRACTED in STREAM_ASSIGN." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$STREAM_TAP" "$STREAM_ASSIGN"
bd dep add "$STREAM_TAP" "$CONVERT_CORE"
bd dep add "$STREAM_TAP" "$EMITTER_EVENTS"

STREAM_TESTS=$(bd create "stream: golden tests via fake StreamReader (text/reasoning/tool-call interleave, concat result)" \
  -d "Drive the tap with the fake eino StreamReader (stable tc.Index). Assert normalized event sequence and the concatenated schema.Message (incl. Extra preservation). Cover buffered tool-call keying and the no-duplicate-proposal contract." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$STREAM_TESTS" "$STREAM_TAP"
bd dep add "$STREAM_TESTS" "$HARNESS_FAKE"

# ========================================
# Phase 6: tools package — surgical split WITHIN runconfig.go.
# Extract ONLY: clientToolInfos, toJSONSchema, classifyToolCalls.
# LEAVE BEHIND: RunConfig, ToolPolicy, route postures, system-prompt constants.
# Files: tools/**, tools/*_test.go
# ========================================

TOOLS_BIND=$(bd create "tools: extract clientToolInfos/toJSONSchema/classifyToolCalls ONLY (no RunConfig/route configs)" \
  -d "Surgical split within runconfig.go. Extract clientToolInfos (AG-UI Tool -> eino ToolInfo), toJSONSchema (JSON-schema parsing via eino-contrib/jsonschema), and classifyToolCalls (client-vs-server classification). LEAVE BEHIND in the app: RunConfig, ToolPolicy, per-route postures (AgenticChatConfig, ToolBasedGenerativeUIConfig, HumanInTheLoopConfig, ...), and system-prompt constants — these are app-specific routing config, out of scope. Validate tool-call ids before emitting." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$TOOLS_BIND" "$GOLDEN"
bd dep add "$TOOLS_BIND" "$PRE_ENSEMBLE"

TOOLS_TESTS=$(bd create "tools: golden tests for schema binding + client/server classification" \
  -d "Table-driven + golden tests for clientToolInfos, toJSONSchema (incl. malformed schema robustness — no panic), and classifyToolCalls. Confirm no RunConfig/route types leaked into the package." \
  -p 1 --parent "$EPIC" --silent)
bd dep add "$TOOLS_TESTS" "$TOOLS_BIND"

# ========================================
# Phase 7: Unit parity gate -> release -> reference-app migration (integration gate).
# Steps 2-3 of the sequencing are one coherent, independently-shippable change.
# ========================================

PARITY_GATE=$(bd create "Unit parity gate: assert the four extracted units match normalized golden fixtures" \
  -d "Run the unit parity comparison across convert/emitter/stream/tools against the ID-masked golden fixtures. This is the acceptance gate at the UNIT level — it does NOT replay the out-of-scope Run() loop, State/docstate snapshots, agent_complete CUSTOM event, approval activities, or MESSAGES_SNAPSHOT. All four package test suites must be green." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$PARITY_GATE" "$CONVERT_TESTS"
bd dep add "$PARITY_GATE" "$EMITTER_TESTS"
bd dep add "$PARITY_GATE" "$EMITTER_TRANSPORT_TEST"
bd dep add "$PARITY_GATE" "$STREAM_TESTS"
bd dep add "$PARITY_GATE" "$TOOLS_TESTS"

RELEASE_TAG=$(bd create "Tag a semver release of the library" \
  -d "Cut a semantic-versioned tag once CI is green, docs are in place, and the unit parity gate passes. This tagged release is what the reference app migrates onto." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$RELEASE_TAG" "$PARITY_GATE"
bd dep add "$RELEASE_TAG" "$SETUP_CI"

MIGRATE_REF=$(bd create "Migrate ag-ui-go-server-example onto the library (replace internal/agent copies)" \
  -d "In ag-ui-go-server-example: add the library dependency (go.mod replace for local dev, then published path), swap imports to convert/emitter/stream/tools, and delete the now-duplicated internal/agent copies. Wire the explicit bufio+SSEWriter+cancel constructor and the injected vision predicate. Files touched: internal/agent/**, go.mod." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$MIGRATE_REF" "$RELEASE_TAG"

VERIFY_REF=$(bd create "Integration gate: ag-ui-go-server-example tests + manual SSE smoke run pass post-migration" \
  -d "Run the reference app's existing test suite and a manual SSE smoke run after migration. No regressions. This is the integration acceptance gate, run AFTER the unit parity gate and the tagged release." \
  -p 0 --parent "$EPIC" --silent)
bd dep add "$VERIFY_REF" "$MIGRATE_REF"

# ========================================
# Phase 8: Docs & runnable example.
# Files: README.md, docs/**, examples/**
# ========================================

DOCS_README=$(bd create "Write README: install/replace instructions, lint/test invocation, version expectations" \
  -d "Cover go.mod replace for local dev + the published module path, pinned eino/SDK versions, and how to run lint/vet/test. State the explicit (non-io.Writer) Emitter constructor contract." \
  -p 2 --parent "$EPIC" --silent)
bd dep add "$DOCS_README" "$EMITTER_CORE"

DOCS_ARCH=$(bd create "Write architecture note mapping each library package back to its origin file" \
  -d "Short note: convert <- convert.go, emitter <- emitter.go, stream <- loop.go:streamTurn(), tools <- runconfig.go (clientToolInfos/toJSONSchema/classifyToolCalls). Record what deliberately stays in the apps (RunConfig, route configs, state machinery, HTTP wiring)." \
  -p 2 --parent "$EPIC" --silent)
bd dep add "$DOCS_ARCH" "$CONVERT_CORE"
bd dep add "$DOCS_ARCH" "$EMITTER_CORE"
bd dep add "$DOCS_ARCH" "$STREAM_TAP"
bd dep add "$DOCS_ARCH" "$TOOLS_BIND"

DOCS_EXAMPLE=$(bd create "Add minimal runnable example: eino model stream driving the Emitter" \
  -d "examples/ shows an eino model StreamReader driving the tap+Emitter. The example wraps an io.Writer into the *bufio.Writer + *sse.SSEWriter the constructor requires — it MUST NOT imply a bare-io.Writer constructor." \
  -p 2 --parent "$EPIC" --silent)
bd dep add "$DOCS_EXAMPLE" "$EMITTER_CORE"
bd dep add "$DOCS_EXAMPLE" "$STREAM_TAP"

# RELEASE_TAG should ship with docs in place.
bd dep add "$RELEASE_TAG" "$DOCS_README"
bd dep add "$RELEASE_TAG" "$DOCS_ARCH"
bd dep add "$RELEASE_TAG" "$DOCS_EXAMPLE"

# ========================================
# Phase 9: Consumption requests — filed AFTER the library is green + reference app migrated.
# Must NOT block the library deliverable (steps 1-3). The ensemble request also depends on the
# externally-confirmed ensemble repo path.
# Files: ~/.agents/projects/<repo>/requests
# ========================================

REQ_REF=$(bd create "File consumption request for ag-ui-go-server-example adoption" \
  -d "In ~/.agents/projects/ag-ui-go-server-example/requests: describe module path, replace directive, import swaps, and version expectations. (Migration already proven in VERIFY_REF — this records the canonical adoption recipe.)" \
  -p 2 --parent "$EPIC" --silent)
bd dep add "$REQ_REF" "$VERIFY_REF"

REQ_ENSEMBLE=$(bd create "File consumption request for the ensemble Go backend adoption" \
  -d "In ~/.agents/projects/<ensemble-backend-repo>/requests (confirm the real repo path/name first, per PRE_ENSEMBLE): describe module path, replace directive, import swaps, and version expectations — including the eino-floor implications from PRE_EINO (forced MVS bump). Must not block the library deliverable." \
  -p 3 --parent "$EPIC" --silent)
bd dep add "$REQ_ENSEMBLE" "$VERIFY_REF"
bd dep add "$REQ_ENSEMBLE" "$PRE_ENSEMBLE"

echo ""
echo "Bead graph created! View with:"
echo "  bd show $EPIC          # The parent epic and its rollup"
echo "  bd children $EPIC      # All task beads under the epic"
echo "  bd dep tree            # Ordering edges between tasks (epic has none)"
echo "  bd ready               # Unblocked tasks (the epic itself is excluded)"
