// Package stream provides the reusable classic eino stream to AG-UI event tap.
// StreamTurn returns provider output plus the exact wire transcript, stable
// tool owner, observed usage, and partial-result state.
//
// If live tool-call streaming is enabled, callers must not also emit post-turn
// tool proposals for the same calls.
//
// StreamAgenticTurn drains model.AgenticModel with indexed block correlation.
// DrainAgenticEvents consumes typed ADK observations through an explicit
// abort-and-join source boundary. Observer failure detaches without cancelling
// the host-owned execution context.
package stream
