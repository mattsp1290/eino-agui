package stream

import (
	"context"
	"errors"
	"io"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/emitter"
)

// Result preserves both the provider-facing Eino response and the exact
// message identities represented by the emitted AG-UI stream.
type Result struct {
	// Assistant is the concatenated provider response. It is nil when no
	// response can be concatenated, including an empty stream.
	Assistant *schema.Message
	// WireMessages contains completed AG-UI messages whose frames were written
	// successfully, plus the stable owner for correlated tool calls.
	WireMessages []aguitypes.Message
	// ToolOwnerID identifies the WireMessages entry that owns correlated tool
	// calls. It is empty when the stream contains no correlatable tool calls.
	ToolOwnerID string
	// Usage is the per-field maximum of cumulative usage observations received
	// from the provider. It is retained on partial results.
	Usage *schema.TokenUsage
	// Partial reports that the stream opened but did not complete successfully.
	Partial bool
}

// CorrelationError reports an ambiguous streamed tool-call identity.
type CorrelationError struct{ Message string }

func (e *CorrelationError) Error() string { return "tool-call correlation: " + e.Message }

// Option configures StreamTurn.
type Option func(*config)

type config struct{ liveToolCalls bool }

// WithLiveToolCallEvents controls whether streamed model tool calls are emitted
// live as TOOL_CALL_* events. When enabled, callers must not also emit post-turn
// tool proposals for the same calls.
func WithLiveToolCallEvents(enabled bool) Option {
	return func(cfg *config) { cfg.liveToolCalls = enabled }
}

// StreamTurn streams one classic Eino model turn and returns its provider
// response together with the AG-UI wire transcript and tool-owner identity.
// Once the model stream opens, errors return a non-nil partial Result.
func StreamTurn(ctx context.Context, emit *emitter.Emitter, cm model.ToolCallingChatModel, messages []*schema.Message, opts ...Option) (*Result, error) {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	sr, err := cm.Stream(ctx, messages)
	if err != nil {
		return nil, err
	}
	defer sr.Close()

	state := newTurnState(emit, cfg.liveToolCalls)
	for {
		if err := ctx.Err(); err != nil {
			return state.finish(err)
		}
		if err := emit.Err(); err != nil {
			return state.finish(err)
		}
		chunk, recvErr := sr.Recv()
		if errors.Is(recvErr, io.EOF) {
			return state.finish(nil)
		}
		if recvErr != nil {
			return state.finish(recvErr)
		}
		if err := state.applyChunk(chunk); err != nil {
			return state.finish(err)
		}
	}
}
