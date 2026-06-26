package testids

import (
	"fmt"
	"sync"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

var globalMu sync.Mutex

// TB is the subset of testing.TB needed by this package.
type TB interface {
	Helper()
	Cleanup(func())
}

// Generator emits monotonically increasing, deterministic AG-UI event IDs.
type Generator struct {
	mu     sync.Mutex
	prefix string
	next   int
}

// NewGenerator returns a deterministic ID generator.
//
// When prefix is empty, IDs are formatted as "<kind>-000001". When prefix is
// set, IDs are formatted as "<prefix>-<kind>-000001".
func NewGenerator(prefix string) *Generator {
	return &Generator{prefix: prefix, next: 1}
}

// WithDeterministicGenerator installs a deterministic generator as the SDK
// global for the lifetime of t and restores the previous generator at cleanup.
//
// The SDK global is process-wide and unsynchronized, so this helper holds an
// internal lock until t.Cleanup runs. Tests using it must not call t.Parallel.
func WithDeterministicGenerator(t TB, prefix string) *Generator {
	t.Helper()

	generator := NewGenerator(prefix)
	WithGenerator(t, generator)
	return generator
}

// WithGenerator installs generator as the SDK global for the lifetime of t and
// restores the previous generator at cleanup.
//
// The SDK global is process-wide and unsynchronized, so this helper holds an
// internal lock until t.Cleanup runs. Tests using it must not call t.Parallel.
func WithGenerator(t TB, generator aguievents.IDGenerator) {
	t.Helper()

	if generator == nil {
		panic("testids: nil AG-UI ID generator")
	}

	globalMu.Lock()
	previous := aguievents.GetDefaultIDGenerator()
	aguievents.SetDefaultIDGenerator(generator)

	t.Cleanup(func() {
		aguievents.SetDefaultIDGenerator(previous)
		globalMu.Unlock()
	})
}

// GenerateRunID generates a deterministic run ID.
func (g *Generator) GenerateRunID() string {
	return g.generate("run")
}

// GenerateMessageID generates a deterministic message ID.
func (g *Generator) GenerateMessageID() string {
	return g.generate("msg")
}

// GenerateToolCallID generates a deterministic tool-call ID.
func (g *Generator) GenerateToolCallID() string {
	return g.generate("tool")
}

// GenerateThreadID generates a deterministic thread ID.
func (g *Generator) GenerateThreadID() string {
	return g.generate("thread")
}

// GenerateStepID generates a deterministic step ID.
func (g *Generator) GenerateStepID() string {
	return g.generate("step")
}

func (g *Generator) generate(kind string) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	id := fmt.Sprintf("%s-%06d", kind, g.next)
	if g.prefix != "" {
		id = g.prefix + "-" + id
	}
	g.next++
	return id
}
