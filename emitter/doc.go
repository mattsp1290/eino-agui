// Package emitter provides typed AG-UI SSE event emission helpers for eino
// agent streams.
//
// NewEmitter binds the caller-provided *bufio.Writer, AG-UI *sse.SSEWriter,
// thread/run IDs, and optional context.CancelFunc. The constructor deliberately
// does not accept a generic io.Writer: callers own wrapping their transport into
// the concrete buffered writer pair used by the AG-UI SDK. Typed helpers and
// caller-built events sent through Emit share one error-classification path.
// Calls must be serialized by the caller.
//
// Agentic helpers distinguish detachable transient observers from
// receipt-gated committed projection and lifecycle emission. They do not
// commit state, authorize interrupts, or infer turn/run settlement.
package emitter
