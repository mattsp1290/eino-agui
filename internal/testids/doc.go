// Package testids provides deterministic AG-UI event ID helpers for tests.
//
// The AG-UI Go SDK currently exposes ID generation through a process-global
// events.IDGenerator. Helpers in this package serialize global overrides so
// fixture tests can opt into stable IDs without racing each other.
package testids
