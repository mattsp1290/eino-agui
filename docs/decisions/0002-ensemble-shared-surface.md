# Decision 0002: Library Scope Is Protocol-Bridge Only

Date: 2026-09-09

## Decision

The current release is scoped to the reusable AG-UI/classic-Eino protocol
bridge. Earlier consumer compatibility constraints are superseded: the user
confirmed there are no active external consumers and backward compatibility is
not required.

The supported surface is message/tool conversion, one-way client tool binding,
typed and generic event emission, and classic `ToolCallingChatModel` streaming.
Application orchestration remains outside the library: HTTP serving, route
policy, persistence, tool execution, approvals, resume, and checkpointing.

Eino AgenticModel/ADK, MCP/server-tool blocks, tool search, and non-image
multimodal conversion require a concrete consumer and separate lifecycle design
before they enter this package.
