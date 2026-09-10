# 0005: Use a closed projection for agentic data

Status: accepted

## Context

Eino v0.9.19 agentic messages contain public content alongside provider
continuation state, encrypted reasoning material, arbitrary extensions, and
host-owned runtime state. Passing the upstream structs through AG-UI would make
the wire contract unstable and could disclose private data.

## Decision

The rich bridge exposes a closed, versioned public model in `convert`. Every
one of Eino v0.9.19's 20 content-block variants and the five nested function
result variants has an explicit projection. Tagged unions must contain exactly
one payload matching their discriminator. Unknown fields and versions fail on
decode.

The single custom event namespace is `eino.agentic.v1`. Standard AG-UI events
remain the rendering projection for text, reasoning, function calls, and other
SDK-supported content. The custom envelope carries durable identity and rich
semantics the pinned SDK cannot represent. Consumers merge the native and
custom views only when their complete identity tuples match.

Session, run, turn, message, block, attempt, agent-path, call, approval, and
interrupt identities come from the host. The bridge validates them and never
creates durable identity. `threadId` is an AG-UI alias and must equal the host
session ID.

Final projections and lifecycle facts use RFC 8785 canonical JSON and
domain-separated SHA-256 digests. Authoritative emission requires a host
receipt binding the revision, domain or lifecycle kind, identity, and digest.
This binding validates the receipt's relationship to the candidate; it does
not assert that the bridge inspected or committed host storage.

Projection uses explicit positive limits before cloning or encoding. Arbitrary
server-tool values must be acyclic JSON-compatible data. Provider annotations
are allowlisted. Raw `Extra`, arbitrary extension values, reasoning signatures,
provider response/continuation IDs, Claude encrypted citation indices, Gemini
SDK blobs, and ADK checkpoint/private interrupt data are excluded.

## Consequences

The contract is intentionally breaking and must be revised when Eino adds a
content kind or public provider shape. Rich semantics remain lossless within
the public allowlist, while model execution, persistence, approval decisions,
checkpoint ownership, and replay storage stay with the host.

This decision supersedes the agentic/ADK exclusions in decisions 0001 and 0002;
their classic bridge boundaries otherwise remain in force.
