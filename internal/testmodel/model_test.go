package testmodel

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestReplayModelStreamsFixedChunks(t *testing.T) {
	model := NewReplayModel([]*schema.Message{
		TextChunk("Hello "),
		TextChunk("world"),
	})

	for i := 0; i < 2; i++ {
		stream, err := model.Stream(context.Background(), nil)
		if err != nil {
			t.Fatalf("Stream() call %d error = %v", i, err)
		}
		defer stream.Close()

		first, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv() first call %d error = %v", i, err)
		}
		second, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv() second call %d error = %v", i, err)
		}
		if got, want := first.Content+second.Content, "Hello world"; got != want {
			t.Fatalf("streamed content call %d = %q, want %q", i, got, want)
		}
		_, err = stream.Recv()
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Recv() final call %d error = %v, want io.EOF", i, err)
		}
	}
	if got, want := model.Calls(), 2; got != want {
		t.Fatalf("Calls() = %d, want %d", got, want)
	}
}

func TestScriptedModelConsumesTurnsAndGeneratesConcat(t *testing.T) {
	model := NewScriptedModel(
		[]*schema.Message{TextChunk("first")},
		[]*schema.Message{TextChunk("second")},
	)

	first, err := model.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate() first error = %v", err)
	}
	second, err := model.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate() second error = %v", err)
	}
	if first.Content != "first" || second.Content != "second" {
		t.Fatalf("Generate() contents = %q, %q; want first, second", first.Content, second.Content)
	}
	if _, err := model.Generate(context.Background(), nil); err == nil {
		t.Fatal("Generate() after turns exhausted error = nil, want error")
	}
}

func TestToolCallChunksUseStableIndexPointer(t *testing.T) {
	chunks := ToolCallChunks(7, "call-1", "search", `{"q":`, `"eino"}`)
	if got, want := len(chunks), 2; got != want {
		t.Fatalf("len(ToolCallChunks()) = %d, want %d", got, want)
	}

	first := chunks[0].ToolCalls[0].Index
	second := chunks[1].ToolCalls[0].Index
	if first == nil || second == nil {
		t.Fatalf("tool-call indexes = %v, %v; want non-nil", first, second)
	}
	if first != second {
		t.Fatal("tool-call chunks do not share the same Index pointer")
	}
}

func TestStreamClonesPreserveStableIndexPerReplay(t *testing.T) {
	model := NewReplayModel(ToolCallChunks(3, "call-1", "search", `{"q":`, `"eino"}`))

	first := streamAll(t, model)
	second := streamAll(t, model)

	firstIndex0 := first[0].ToolCalls[0].Index
	firstIndex1 := first[1].ToolCalls[0].Index
	secondIndex0 := second[0].ToolCalls[0].Index
	secondIndex1 := second[1].ToolCalls[0].Index

	if firstIndex0 != firstIndex1 {
		t.Fatal("first replay did not preserve stable Index pointer across chunks")
	}
	if secondIndex0 != secondIndex1 {
		t.Fatal("second replay did not preserve stable Index pointer across chunks")
	}
	if firstIndex0 == secondIndex0 {
		t.Fatal("separate replays share Index pointer aliases")
	}
}

func TestWithToolsSharesScriptedState(t *testing.T) {
	base := NewScriptedModel(
		[]*schema.Message{TextChunk("first")},
		[]*schema.Message{TextChunk("second")},
	)

	firstModel, err := base.WithTools([]*schema.ToolInfo{{Name: "search"}})
	if err != nil {
		t.Fatalf("WithTools() first error = %v", err)
	}
	secondModel, err := base.WithTools([]*schema.ToolInfo{{Name: "search"}})
	if err != nil {
		t.Fatalf("WithTools() second error = %v", err)
	}

	first, err := firstModel.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate() first bound model error = %v", err)
	}
	second, err := secondModel.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate() second bound model error = %v", err)
	}
	if first.Content != "first" || second.Content != "second" {
		t.Fatalf("bound Generate() contents = %q, %q; want first, second", first.Content, second.Content)
	}
	if got, want := base.Calls(), 2; got != want {
		t.Fatalf("base Calls() = %d, want %d", got, want)
	}
}

func TestReplayModelDeepCopiesEncryptedReasoningParts(t *testing.T) {
	model := NewReplayModel([]*schema.Message{EncryptedReasoningChunk("hidden", "sig-fixture")})

	first := streamAll(t, model)
	firstPart := &first[0].AssistantGenMultiContent[0]
	firstPart.Reasoning.Text = "mutated"
	firstPart.Reasoning.Signature = "mutated-sig"
	firstPart.StreamingMeta.Index = 99

	second := streamAll(t, model)
	secondPart := second[0].AssistantGenMultiContent[0]
	if got, want := secondPart.Reasoning.Text, "hidden"; got != want {
		t.Fatalf("replayed reasoning text = %q, want %q", got, want)
	}
	if got, want := secondPart.Reasoning.Signature, "sig-fixture"; got != want {
		t.Fatalf("replayed reasoning signature = %q, want %q", got, want)
	}
	if got, want := secondPart.StreamingMeta.Index, 0; got != want {
		t.Fatalf("replayed streaming meta index = %d, want %d", got, want)
	}
}

func TestMixedStreamChunksCoverFixtureCases(t *testing.T) {
	chunks := MixedStreamChunks()

	var hasText, hasReasoning, hasEncryptedReasoning, hasToolCall bool
	for _, chunk := range chunks {
		hasText = hasText || chunk.Content != ""
		hasReasoning = hasReasoning || chunk.ReasoningContent != ""
		hasToolCall = hasToolCall || len(chunk.ToolCalls) > 0
		for _, part := range chunk.AssistantGenMultiContent {
			hasEncryptedReasoning = hasEncryptedReasoning || part.Reasoning != nil && part.Reasoning.Signature != ""
		}
	}

	if !hasText || !hasReasoning || !hasEncryptedReasoning || !hasToolCall {
		t.Fatalf("coverage text=%v reasoning=%v encrypted=%v tool=%v; want all true",
			hasText, hasReasoning, hasEncryptedReasoning, hasToolCall)
	}
}

func streamAll(t *testing.T, model *Model) []*schema.Message {
	t.Helper()

	stream, err := model.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer stream.Close()

	var chunks []*schema.Message
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return chunks
		}
		if err != nil {
			t.Fatalf("Recv() error = %v", err)
		}
		chunks = append(chunks, chunk)
	}
}
