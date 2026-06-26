package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/emitter"
	"github.com/mattsp1290/eino-agui/stream"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out io.Writer) error {
	writer := bufio.NewWriter(out)
	emit := emitter.NewEmitter(ctx, writer, sse.NewSSEWriter(), "thread-example", "run-example", nil)
	model := replayModel{chunks: exampleChunks()}

	emit.RunStarted()
	msg, err := stream.StreamTurn(ctx, emit, model, nil, stream.WithLiveToolCallEvents(true))
	if err != nil {
		emit.RunError(err.Error())
		_ = writer.Flush()
		return err
	}
	emit.RunFinishedSuccess()
	if err := writer.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "assistant content: %q\n", msg.Content)
	return nil
}

func exampleChunks() []*schema.Message {
	index := 0
	return []*schema.Message{
		{Role: schema.Assistant, ReasoningContent: "checking input "},
		{Role: schema.Assistant, Content: "The weather lookup is "},
		{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				Index: &index,
				ID:    "call-weather",
				Type:  "function",
				Function: schema.FunctionCall{
					Name:      "lookup_weather",
					Arguments: "{\"city\":",
				},
			}},
		},
		{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				Index: &index,
				Type:  "function",
				Function: schema.FunctionCall{
					Arguments: "\"NYC\"}",
				},
			}},
		},
		{Role: schema.Assistant, Content: "ready."},
	}
}

type replayModel struct {
	chunks []*schema.Message
}

func (m replayModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.ConcatMessages(cloneMessages(m.chunks))
}

func (m replayModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray(cloneMessages(m.chunks)), nil
}

func (m replayModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return replayModel{chunks: cloneMessages(m.chunks)}, nil
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	cloned := make([]*schema.Message, len(messages))
	indexes := map[int]*int{}
	for i, msg := range messages {
		next := *msg
		if msg.ToolCalls != nil {
			next.ToolCalls = make([]schema.ToolCall, len(msg.ToolCalls))
			copy(next.ToolCalls, msg.ToolCalls)
			for j := range next.ToolCalls {
				if next.ToolCalls[j].Index == nil {
					continue
				}
				value := *next.ToolCalls[j].Index
				stable := indexes[value]
				if stable == nil {
					v := value
					stable = &v
					indexes[value] = stable
				}
				next.ToolCalls[j].Index = stable
			}
		}
		cloned[i] = &next
	}
	return cloned
}
