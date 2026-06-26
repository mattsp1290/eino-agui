// Package testsse provides byte-capturing SSE helpers for tests.
//
// The helpers wrap the pinned AG-UI SDK SSE writer and a bufio.Writer so tests
// exercise the same framing and flush path as the application emitter.
package testsse
