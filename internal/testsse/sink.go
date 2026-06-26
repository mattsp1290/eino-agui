package testsse

import (
	"bufio"
	"bytes"
	"context"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
)

var frameDelimiter = []byte("\n\n")

// Sink captures AG-UI SSE frames in memory.
type Sink struct {
	buffer bytes.Buffer
	writer *bufio.Writer
	sse    *sse.SSEWriter
}

// NewSink returns a byte-capturing sink backed by the AG-UI SDK SSE writer.
func NewSink() *Sink {
	sink := &Sink{
		sse: sse.NewSSEWriter(),
	}
	sink.writer = bufio.NewWriter(&sink.buffer)
	return sink
}

// Writer returns the bufio writer to pass into emitter code under test.
func (s *Sink) Writer() *bufio.Writer {
	return s.writer
}

// SSEWriter returns the AG-UI SDK SSE writer to pass into emitter code under test.
func (s *Sink) SSEWriter() *sse.SSEWriter {
	return s.sse
}

// WriteEvent writes event through the pinned AG-UI SDK SSE writer.
func (s *Sink) WriteEvent(ctx context.Context, event aguievents.Event) error {
	return s.sse.WriteEvent(ctx, s.writer, event)
}

// WriteBytes writes already-encoded event bytes through the pinned AG-UI SDK
// SSE writer.
func (s *Sink) WriteBytes(ctx context.Context, event []byte) error {
	return s.sse.WriteBytes(ctx, s.writer, event)
}

// Flush flushes buffered bytes into the captured output.
func (s *Sink) Flush() error {
	return s.writer.Flush()
}

// Bytes returns a copy of the captured SSE bytes.
func (s *Sink) Bytes() []byte {
	_ = s.Flush()
	return append([]byte(nil), s.buffer.Bytes()...)
}

// String returns the captured SSE bytes as a string.
func (s *Sink) String() string {
	return string(s.Bytes())
}

// Frames returns captured SSE frames, preserving each trailing blank-line
// delimiter.
func (s *Sink) Frames() [][]byte {
	data := s.Bytes()
	if len(data) == 0 {
		return nil
	}

	parts := bytes.SplitAfter(data, frameDelimiter)
	frames := make([][]byte, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		frames = append(frames, append([]byte(nil), part...))
	}
	return frames
}

// Reset clears captured output and resets the buffered writer.
func (s *Sink) Reset() {
	_ = s.Flush()
	s.buffer.Reset()
	s.writer.Reset(&s.buffer)
}
