package testsse

import (
	"bytes"
	"context"
	"strings"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

func TestSinkWritesEventThroughSDKSSEWriter(t *testing.T) {
	sink := NewSink()

	if sink.Writer() == nil {
		t.Fatal("Writer() returned nil")
	}
	if sink.SSEWriter() == nil {
		t.Fatal("SSEWriter() returned nil")
	}

	err := sink.WriteEvent(context.Background(), aguievents.NewRunStartedEvent("thread-1", "run-1"))
	if err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	got := sink.String()
	for _, want := range []string{
		"id: RUN_STARTED_",
		"data: ",
		`"type":"RUN_STARTED"`,
		`"threadId":"thread-1"`,
		`"runId":"run-1"`,
		"\n\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("captured SSE frame missing %q:\n%s", want, got)
		}
	}
}

func TestSinkWriteBytesUsesSDKFramingAndEscaping(t *testing.T) {
	sink := NewSink()

	err := sink.WriteBytes(context.Background(), []byte("{\"line\":\"a\nb\"}"))
	if err != nil {
		t.Fatalf("WriteBytes() error = %v", err)
	}

	want := "data: {\"line\":\"a\\nb\"}\n\n"
	if got := sink.String(); got != want {
		t.Fatalf("captured bytes = %q, want %q", got, want)
	}
}

func TestSinkFramesPreserveDelimitersAndReturnCopies(t *testing.T) {
	sink := NewSink()

	if err := sink.WriteEvent(context.Background(), aguievents.NewRunStartedEvent("thread-1", "run-1")); err != nil {
		t.Fatalf("WriteEvent() first error = %v", err)
	}
	if err := sink.WriteEvent(context.Background(), aguievents.NewRunFinishedEvent("thread-1", "run-1")); err != nil {
		t.Fatalf("WriteEvent() second error = %v", err)
	}

	frames := sink.Frames()
	if got, want := len(frames), 2; got != want {
		t.Fatalf("len(Frames()) = %d, want %d", got, want)
	}
	for i, frame := range frames {
		if !bytes.HasSuffix(frame, frameDelimiter) {
			t.Fatalf("frame %d missing delimiter: %q", i, frame)
		}
	}
	if got := bytes.Join(frames, nil); !bytes.Equal(got, sink.Bytes()) {
		t.Fatalf("joined frames = %q, want captured bytes %q", got, sink.Bytes())
	}

	frames[0][0] = 'X'
	if bytes.Equal(frames[0], sink.Frames()[0]) {
		t.Fatal("Frames() returned aliases of captured bytes")
	}
}

func TestSinkResetClearsCapturedOutput(t *testing.T) {
	sink := NewSink()

	if err := sink.WriteBytes(context.Background(), []byte(`{"type":"CUSTOM"}`)); err != nil {
		t.Fatalf("WriteBytes() error = %v", err)
	}
	sink.Reset()

	if got := sink.Bytes(); len(got) != 0 {
		t.Fatalf("Bytes() after Reset() = %q, want empty", got)
	}
	if got := sink.Frames(); got != nil {
		t.Fatalf("Frames() after Reset() = %#v, want nil", got)
	}
}
