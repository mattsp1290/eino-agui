# Project Planning with Beads

## Agent Instructions

You are an expert software architect creating a comprehensive task breakdown. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph. Include all necessary setup, implementation, testing, and documentation tasks. Go beyond the basics - consider edge cases, error handling, security considerations, and integration points. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

## Project Information

### Links to Relevant Documentation

- **Primary reference / source of the code being extracted:** `~/git/ag-ui-go-server-example`
  (module `github.com/mattsp1290/ag-ui-go-server-example`, Go 1.26, eino v0.9.2). The integration
  seam to extract lives in `internal/agent/`: `convert.go` (eino↔AG-UI message/tool conversion),
  `emitter.go` (typed SSE event emitter over `*bufio.Writer` + AG-UI `sse.SSEWriter`),
  `loop.go:streamTurn()` (eino `StreamReader` → AG-UI events tap), `runconfig.go` (AG-UI `Tool` →
  eino `ToolInfo` / JSON-schema binding, `classifyToolCalls()`), and `tools.go` (toolset binding).
- **Second consumer (Go backend):** the **ensemble** coding-agent orchestrator server. NOTE:
  `~/git/ensemble-ui` is the **Flutter/Dart frontend** (consumes the AG-UI SSE stream); the Go
  backend that emits AG-UI events is a separate service and is **not** checked out under `~/git`.
  **Hard precondition (blocking):** locating/checking out this repo, AND extracting its currently
  duplicated eino↔AG-UI functions and diffing them against `ag-ui-go-server-example`'s versions, is
  a blocking precondition for finalizing the public API. Confirming a path is **not** the same as
  validating API fit — the shared surface must be *derived from both consumers*, not assumed from
  one. If ensemble's code has diverged enough that the abstraction doesn't fit, that finding must be
  recorded and the scope renegotiated before extraction proceeds.
- **AG-UI Go SDK:** `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — packages
  `pkg/core/events`, `pkg/core/types`, `pkg/encoding/sse`. **Caution:** the reference app consumes
  this via a `go.mod replace` to a **local, untagged fork** at `~/git/ag-ui`
  (pseudo-version `v0.0.0-…`). A published library **cannot** ship a `replace` to a local fork, so a
  task must (a) pin a real published commit/tag of the SDK, (b) verify the public SDK matches the
  local fork the seam relies on (no unpushed patches the converters/emitter depend on), and (c) pin
  the **same SDK version across both consumers** — `isTransportError` depends on SDK-internal error
  strings, so an SDK version skew can silently break disconnect detection.
- **eino framework:** `github.com/cloudwego/eino` v0.9.2 — packages `components/model`,
  `components/tool`, `schema`; plus `github.com/eino-contrib/jsonschema` for tool params.
- **Consumption-request inbox convention:** `~/.agents/projects/<repo-name>/requests` — used to
  ask each consumer repo (`ag-ui-go-server-example`, the ensemble backend) to migrate onto this
  library.

### Project Description

**eino-agui** — a Go library (`github.com/mattsp1290/eino-agui`) that extracts the shared AG-UI +
eino integration code currently duplicated across `~/git/ag-ui-go-server-example` and the ensemble
Go backend, so both can consume one canonical implementation.

**Scope: the "core seam" only.** The library packages the reusable primitives and deliberately
leaves per-application route/business logic in each app:

1. **Message/tool conversion** — bidirectional `eino schema.Message` ↔ AG-UI `types.Message`,
   including multimodal/vision handling and tool-call conversion (extracted from `convert.go`:
   `toEinoMessages`, `toEinoUserMessage`, `toEinoImagePart`, `toAGUIMessages`, `toEinoToolCalls`,
   `toAGUIToolCalls`, `messageText`). **Vision gating must be parameterized**, not hardcoded: today
   `supportsVision()` literally returns `provider == "openai"`. A shared library must expose vision
   capability as an **injected predicate / capability option** (default: a documented allowlist), so
   a second consumer with different provider names neither silently loses nor wrongly enables images.
2. **Typed SSE Emitter** — a wrapper over AG-UI's `sse.SSEWriter` exposing typed methods for every
   event family (run lifecycle, text, reasoning incl. encrypted, tool calls with buffered START
   semantics, state snapshot/delta, messages snapshot, activity, steps, custom), plus
   transport-vs-encoding error separation and client-disconnect detection (extracted from
   `emitter.go`). **Constructor contract is explicit, not "io.Writer-generic":** today's
   `NewEmitter` binds `*bufio.Writer` + `*sse.SSEWriter` + a `cancel context.CancelFunc` (the
   disconnect design is shaped by fasthttp, which only signals disconnect via a failed SSE write).
   The library must state this exact signature; the "runnable example" shows the caller wrapping an
   `io.Writer` into `bufio`/`SSEWriter` — it does **not** imply a generic `io.Writer` constructor.
3. **eino-stream → AG-UI tap** — a reusable equivalent of `streamTurn()` that consumes an eino
   `StreamReader`, emits text/reasoning/tool-call events live (keyed on `toolCallKey`→`*tc.Index`),
   and returns the concatenated `schema.Message` — exposed without the app-specific agent-loop
   business logic. **The tap carries a cross-cut contract** ("callers MUST NOT also emit a tool
   proposal for the same calls"); the plan must explicitly assign each adjacent helper to
   *extracted* or *stays-in-app*: `emitToolProposal`, `validateToolCalls`, `validateToolCallsQuiet`,
   `settlePendingToolCalls`. Leaving these unassigned re-introduces the duplication the library is
   meant to remove.
4. **Tool-schema binding** — AG-UI `Tool` → eino `ToolInfo`, JSON-schema parsing, and
   client-vs-server tool classification. **This is a surgical split *within* `runconfig.go`:**
   extract only `clientToolInfos`, `toJSONSchema`, and `classifyToolCalls`; **leave behind**
   `RunConfig`, `ToolPolicy`, the per-route postures (`AgenticChatConfig`,
   `ToolBasedGenerativeUIConfig`, `HumanInTheLoopConfig`, …) and system-prompt constants, which are
   app-specific routing config (declared out of scope below). The task graph must name the moving
   functions so an agent does not drag route configs into the library.

Explicitly **out of scope** for this library: the full `Run()` agent loop, route configs
(`/human_in_the_loop`, `/shared_state`, `/predictive_state_updates`, etc.), `state.go`/`docstate.go`
state machinery, the run-store/interrupt persistence, the HTTP framework wiring (gofiber), and any
model-provider selection. These stay in the consuming applications.

The deliverable includes migrating `ag-ui-go-server-example` onto the library to prove parity, and
filing consumption requests (via `~/.agents/projects/<repo>/requests`) for the ensemble backend.

### Technical Stack

- **Language:** Go 1.26 (match `ag-ui-go-server-example`'s toolchain).
- **Module:** `github.com/mattsp1290/eino-agui`. Library-only — **no `main` package, no HTTP
  framework dependency** (gofiber stays in the apps). Consumers wire it via `go.mod replace` during
  local dev, then the published module path.
- **Core dependencies:**
  - `github.com/cloudwego/eino` **v0.9.2** (target version) — `components/model`,
    `components/tool`, `schema`.
  - `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — `pkg/core/events`, `pkg/core/types`,
    `pkg/encoding/sse`.
  - `github.com/eino-contrib/jsonschema` — tool-parameter JSON Schema parsing.
- **Transport surface:** the Emitter writes to a `*bufio.Writer` via AG-UI's `sse.SSEWriter` and
  takes a `cancel context.CancelFunc` (exact current `NewEmitter` shape). There is no framework
  *import* coupling, so any handler that can hand over a `*bufio.Writer` (gofiber, net/http, …) can
  drive it — but the constructor is **not** a bare `io.Writer`; callers wrap their writer themselves.
- **Tooling:** Go modules, `go test` (table-driven + golden SSE-frame fixtures), `go vet`,
  `gofmt`/`goimports`, `golangci-lint`, and a GitHub Actions CI workflow (build + vet + lint +
  test). Semantic-versioned tags.

### Specific Requirements

- **Behavioral parity — defined measurably (NOT "byte-for-byte"):** raw byte-for-byte comparison is
  **impossible** as a gate, because every message/text/reasoning/activity ID is minted at runtime by
  the SDK's non-deterministic `aguievents.GenerateMessageID()` (used in `streamTurn`,
  `toAGUIMessages`, `validateToolCalls`, `ActivitySnapshot`, `ToolResult`, …). The parity gate is
  therefore **normalized structural equivalence**: compare event sequences after masking/normalizing
  generated IDs (and any timestamps). To make fixtures deterministic, the harness must provide three
  pieces, each its own task: (1) a **deterministic ID generator** injected for tests — note the only
  current seam, `events.SetDefaultIDGenerator`, is **process-global**, so test isolation/concurrency
  must be designed for (or a per-Emitter ID-generator option added); (2) a **fake eino
  `StreamReader`/`ToolCallingChatModel`** emitting a fixed chunk sequence with **stable `tc.Index`
  pointers** (the tap keys tool calls on `*tc.Index`); (3) a **byte-capturing `SSEWriter` sink**.
  Behaviors to lock: reasoning incl. encrypted-reasoning scrubbing in MESSAGES_SNAPSHOT, tool-call
  buffered-START (emit `TOOL_CALL_START` only once id+name are both known), block-ordering (close
  text/reasoning before tool-call events), and parameterized vision gating.
- **Parity is measured at the UNIT level, not full-app:** a full event-stream replay would exercise
  the **out-of-scope** `Run()` loop, `State`/`docstate` snapshots, the `agent_complete` CUSTOM event,
  approval activities, and MESSAGES_SNAPSHOT — code that is *not* being extracted. The acceptance
  gate compares the **four extracted units** (converters, emitter, stream tap, tool binding) against
  golden fixtures captured from the current implementation of *those same functions* — not against a
  whole-server run.
- **No regressions in the reference app:** after extraction, migrate `ag-ui-go-server-example` to
  import the library (replacing its `internal/agent` copies) and confirm its existing tests + a
  manual SSE smoke run still pass. This is the integration acceptance gate, run *after* the unit
  parity gate above.
- **Clean public API / encapsulation:** export a deliberate surface (e.g. `convert`, `emitter`,
  `stream`, `tools` sub-packages) with doc comments; keep app-specific concerns unexported or out.
  Avoid leaking gofiber, run-store, or route types into the public API.
- **eino version compatibility — a BLOCKING precondition, not a "cost":** target v0.9.2, but under
  Go module MVS a consumer that imports this library is **force-bumped up** to v0.9.2 regardless of
  its own pin. The ensemble backend / `eino-providers` / `eino-tools` sit on v0.8.13, so this can
  break their build — it is a potential **adoption blocker**. Before pinning the eino floor, a task
  must: (a) enumerate the exact v0.9.x-only symbols the seam touches (`schema.MessageInputPart`,
  `schema.MessageInputImage`, `UserInputMultiContent`, `schema.ChatMessagePartTypeImageURL`,
  `chunk.ReasoningContent`, `Extra`-preserving `schema.ConcatMessages`) and confirm which actually
  exist in v0.8.13; (b) determine whether ensemble can absorb a forced 0.9.2 bump. Only then is the
  floor decided. If v0.8.13 lacks required symbols AND ensemble cannot bump, the scope must be
  renegotiated (e.g. drop the features that force 0.9.2, or accept ensemble-first migration).
- **Consumption requests:** after the library is green and the reference app is migrated, file
  change-requests in `~/.agents/projects/ag-ui-go-server-example/requests` and
  `~/.agents/projects/<ensemble-backend-repo>/requests` describing exactly how to adopt the library
  (module path, replace directive, import swaps, version expectations). Confirm the ensemble
  backend repo's real path/name first.
- **Security / robustness:** preserve transport-error vs encoding-error separation (a client
  disconnect cancels the run context; an encoding/validation error drops the event but keeps the
  stream alive). Do not panic on malformed AG-UI input; validate tool-call ids before emitting.
  Ensure no encrypted reasoning content leaks to clients via snapshots. **Known fragility to harden:**
  `isTransportError` classifies disconnects by string-matching the SDK's internal wrapper prefixes
  (`"SSE write failed"`, `"SSE flush failed"`). Add a **regression test that fails if those prefixes
  change** in the pinned SDK, and evaluate upstreaming a typed/sentinel error to the AG-UI SDK rather
  than carrying brittle string matching into a shared library.
- **Sequencing (so the deliverable can't stall on unknowns):** order the work as explicit
  dependencies — (1) preconditions: confirm/diff ensemble seam + decide eino floor + pin SDK
  version; (2) build the four extracted packages + deterministic golden harness; (3) tag a release
  and migrate-and-verify `ag-ui-go-server-example` (parity gate); (4) file consumption requests.
  Steps 2–3 are one coherent, independently-shippable change; step 4 depends on external confirmation
  of the ensemble repo and must not block 1–3.
- **Docs:** a `README.md` with install/replace instructions, a short architecture note mapping each
  library package back to its origin file, and at least one minimal runnable example (`examples/`)
  showing an eino model stream driving the Emitter — the example wraps an `io.Writer` into the
  `*bufio.Writer` + `sse.SSEWriter` the constructor requires (it does not imply a bare-`io.Writer`
  constructor).

---

## Your Task

Analyze this project and create a comprehensive **Beads task graph** using the `bd` CLI. Beads provides dependency-aware, conflict-free task management for multi-agent execution.

---

<critical_constraint>
Your ONLY output is a bash shell script containing `bd create` and `bd dep add` commands. Do NOT use `bd add` — the correct command to create a bead is `bd create`. Use `bd dep add` for dependencies between task beads. Do not implement anything yourself.

The script MUST create a single parent **epic** first (`bd create -t epic`) and parent **every** task bead to it via `--parent "$EPIC"`, so the whole project is one trackable rollup. The epic is an organizational rollup only — never make it a blocking dependency (do NOT `bd dep add` to or from the epic; `bd dep add` is for real ordering edges between task beads, and a blocking edge on an epic both excludes it wrongly and inverts `bd dep tree`). Membership is the `--parent` relationship, nothing else.
</critical_constraint>

## Output Format

Generate a shell script that creates the full task graph. The script should:

1. **Initialize Beads** (if not already initialized)
2. **Create one parent epic** (`bd create -t epic`) representing the whole project, capturing its ID into `$EPIC`
3. **Create all task beads** with appropriate priorities, each parented to the epic via `--parent "$EPIC"`
4. **Establish dependencies** between task beads (ordering edges only — never to or from the epic)

### Example Output

```bash
#!/bin/bash
# Project: eino-agui
# Generated: 2026-06-26

set -e

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating project beads..."

# ========================================
# Parent epic — every task below is parented to it (--parent "$EPIC").
# The epic is an organizational rollup: it is NEVER given a blocking dep
# (no `bd dep add` to or from it) and is never dispatched as work itself.
# ========================================

EPIC=$(bd create "Epic: eino-agui" -t epic -p 0 --silent)
bd update "$EPIC" --status in_progress   # rollup, not dispatchable work — keep it out of `bd ready`

# ========================================
# Phase 0: BLOCKING preconditions (resolve unknowns before extraction)
# ========================================

PRE_EINO=$(bd create "Audit eino 0.9.2-only symbols vs 0.8.13 availability; decide version floor" \
  -d "Enumerate schema.MessageInputPart, MessageInputImage, UserInputMultiContent, ChatMessagePartTypeImageURL, chunk.ReasoningContent, Extra-preserving ConcatMessages. Confirm which exist in v0.8.13. Determine if ensemble can absorb a forced MVS bump to 0.9.2. Output: the pinned eino floor + rationale." \
  -p 0 --parent "$EPIC" --silent)

PRE_ENSEMBLE=$(bd create "Locate ensemble Go backend; extract+diff its duplicated eino/AG-UI funcs to derive true shared surface" \
  -d "Confirming a path is not validating API fit. Diff ensemble's converters/emitter/tap against ag-ui-go-server-example to derive the common surface. Record divergence; renegotiate scope if abstraction does not fit both." \
  -p 0 --parent "$EPIC" --silent)

PRE_SDK=$(bd create "Pin a published AG-UI Go SDK commit/tag; verify public == local fork; pin same version across both consumers" \
  -d "Reference app uses go.mod replace to local untagged fork ~/git/ag-ui. A published lib cannot ship that replace. Pin real version, verify no unpushed patches the seam relies on, align both consumers (isTransportError depends on SDK-internal error strings)." \
  -p 0 --parent "$EPIC" --silent)

# ========================================
# Phase 1: Project Setup & Infrastructure
# ========================================

SETUP_MOD=$(bd create "Initialize Go module github.com/mattsp1290/eino-agui (Go 1.26)" -p 0 --parent "$EPIC" --silent)
bd dep add $SETUP_MOD $PRE_EINO
bd dep add $SETUP_MOD $PRE_SDK

SETUP_LINT=$(bd create "Configure golangci-lint, gofmt/goimports, go vet" -p 1 --parent "$EPIC" --silent)
bd dep add $SETUP_LINT $SETUP_MOD

SETUP_CI=$(bd create "Add GitHub Actions CI (build + vet + lint + test)" -p 1 --parent "$EPIC" --silent)
bd dep add $SETUP_CI $SETUP_LINT

# ========================================
# Phase 2: Deterministic golden-parity harness (lock UNIT behavior before extraction)
# ========================================

HARNESS_ID=$(bd create "Add deterministic ID-generator seam for tests (per-Emitter option or guarded SetDefaultIDGenerator)" \
  -d "GenerateMessageID is non-deterministic and global. Provide a deterministic generator for fixtures; design for test isolation since events.SetDefaultIDGenerator is process-global." \
  -p 0 --parent "$EPIC" --silent)
bd dep add $HARNESS_ID $SETUP_MOD

HARNESS_FAKE=$(bd create "Build fake eino StreamReader/ToolCallingChatModel with fixed chunks + stable tc.Index" -p 0 --parent "$EPIC" --silent)
bd dep add $HARNESS_FAKE $SETUP_MOD

GOLDEN=$(bd create "Capture normalized (ID-masked) golden SSE fixtures for the four extracted units" \
  -d "Parity = normalized structural equivalence, NOT byte-for-byte. Mask generated IDs/timestamps. Capture from current convert/emitter/streamTurn/tool-binding funcs via byte-capturing SSEWriter sink." \
  -p 0 --parent "$EPIC" --silent)
bd dep add $GOLDEN $HARNESS_ID
bd dep add $GOLDEN $HARNESS_FAKE

# ... continue for all phases: convert pkg (parameterized vision predicate), emitter pkg
#     (explicit bufio+SSEWriter+cancel constructor; isTransportError regression test), stream tap
#     (assign emitToolProposal/validateToolCalls* extracted-or-stays), tools binding (clientToolInfos/
#     toJSONSchema/classifyToolCalls only — NOT RunConfig/route configs), tag release, migrate-and-
#     verify reference app (integration gate), docs/examples, then file consumption requests ...

echo ""
echo "Bead graph created! View with:"
echo "  bd show $EPIC          # The parent epic and its rollup"
echo "  bd children $EPIC      # All task beads under the epic"
echo "  bd ready              # List unblocked tasks (the epic itself is not work)"
```

---

## Bead Creation Guidelines

### Epic / Hierarchy (REQUIRED)
- Create exactly **one parent epic** for the whole project: `EPIC=$(bd create "Epic: <project summary>" -t epic -p 0 --silent)`.
- Parent **every** task bead to it: add `--parent "$EPIC"` to every `bd create`.
- The epic is a **rollup, not work**: never `bd dep add` to or from it. Membership is `--parent`; `bd dep add` is reserved for real ordering edges *between task beads*. A blocking edge on an epic wrongly keeps it out of (or drops it into) `bd ready` and inverts `bd dep tree`.
- **Keep the epic out of `bd ready`** by marking it active right after creation: `bd update "$EPIC" --status in_progress`. `bd ready` excludes `in_progress`/`blocked`/`deferred`/`hooked`. Do **not** rely on `--exclude-type epic` — that flag is ineffective on some `bd`/`bn` builds, whereas status-based exclusion works everywhere.
- For very large projects you MAY use phase sub-epics (each `--parent "$EPIC"`, each with its own children), but a single top-level epic is the default and is sufficient for most projects.

### Priority Levels
- `-p 0` = Critical (blocking other work)
- `-p 1` = High (important but not blocking)
- `-p 2` = Medium (standard work)
- `-p 3` = Low (nice to have)

### Dependency Rules
1. Never create cycles
2. Every bead should have a clear dependency chain back to setup tasks
3. Use `bd dep add CHILD PARENT` (child depends on parent completing first)
4. Parallel work should share a common ancestor, not depend on each other
5. `bd dep add` is for ordering edges **between task beads only** — never use it to attach a task to the epic (that is `--parent`), and never add a blocking edge to or from the epic

### Task Granularity
- Each bead should be completable in **under 750 lines of code**
- Tasks should be atomic enough for one agent to complete without coordination
- If a task requires multiple file areas, consider splitting by file area

---

## File Reservation Planning

For each major work area, note the file patterns that will need exclusive reservation:

```bash
# Message conversion:  convert/**, convert/*_test.go
# Emitter:             emitter/**, emitter/*_test.go, testdata/golden/**
# Stream tap:          stream/**, stream/*_test.go
# Tool binding:        tools/**, tools/*_test.go
# Reference migration: (in ag-ui-go-server-example) internal/agent/**, go.mod
# Docs/examples:       README.md, docs/**, examples/**
```

This helps agents claim appropriate file surfaces when they start work.

---

## Context Documentation

Place any important context in `prompts/docs/` for agents to reference. This includes:
- Architecture decisions
- API documentation
- Design system specs
- External service integration guides

---

## Verification Steps

After generating the script:

1. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
2. **Check the rollup**: `bd children "$EPIC"` should list every task bead, and `bd dep tree` should show them under the epic with no orphan (un-parented) tasks
3. **Check ready work**: `bd ready` should show initial setup tasks and **not** the epic. Epics are rollups, never dispatched as work — mark the epic `in_progress` right after creating it so status-based exclusion keeps it out of `bd ready` on every build.

---

## Completeness Checklist

Ensure your task graph includes:

- [ ] A single parent epic (`-t epic`); every task bead parented to it via `--parent "$EPIC"`, with no orphan tasks and no blocking dep to/from the epic
- [ ] **Phase 0 BLOCKING preconditions**: eino floor decision (0.9.2 symbols vs 0.8.13), ensemble repo located + seam diffed, AG-UI SDK pinned to a published version — extraction tasks `bd dep add` onto these
- [ ] All setup and configuration tasks (module init, lint, CI)
- [ ] **Deterministic** golden-parity harness (injectable ID generator, fake eino StreamReader w/ stable tc.Index, byte-capturing SSEWriter) captured BEFORE extraction
- [ ] Parity defined as **normalized/ID-masked structural equivalence at the unit level** — NOT byte-for-byte, NOT a full-`Run()` replay
- [ ] Core architecture and shared utilities (convert w/ **parameterized vision predicate**, emitter w/ **explicit bufio+SSEWriter+cancel constructor**, stream tap, tools binding)
- [ ] Explicit extracted-vs-stays assignment for `emitToolProposal`/`validateToolCalls*`/`settlePendingToolCalls`; tools binding extracts only `clientToolInfos`/`toJSONSchema`/`classifyToolCalls` (NOT `RunConfig`/route configs)
- [ ] Feature implementation tasks (broken into small units per package)
- [ ] Error handling and edge cases (transport vs encoding errors, malformed tool calls, encrypted-reasoning scrubbing)
- [ ] Unit and integration tests for each package; **`isTransportError` regression test** that fails if SDK error-string prefixes change
- [ ] Reference-app migration (ag-ui-go-server-example) as the **integration** gate, sequenced AFTER the unit parity gate + a tagged release
- [ ] Consumption requests filed for ag-ui-go-server-example and the ensemble backend (must not block the library deliverable)
- [ ] API documentation (README, package docs, runnable example that wraps `io.Writer` into bufio+SSEWriter)
- [ ] Security considerations (no encrypted-reasoning leakage, tool-call id validation)
- [ ] CI workflow
- [ ] Clear dependency chains with no cycles
