// Package emitter provides typed AG-UI SSE event emission helpers for eino
// agent streams.
//
// NewEmitter binds the caller-provided *bufio.Writer, AG-UI *sse.SSEWriter,
// thread/run IDs, and optional context.CancelFunc. The constructor deliberately
// does not accept a generic io.Writer: callers own wrapping their transport into
// the concrete buffered writer pair used by the AG-UI SDK.
package emitter
