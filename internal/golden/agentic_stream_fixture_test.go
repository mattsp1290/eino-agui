package golden_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/internal/golden"
)

func TestAgenticStreamGoldenHasStrictCompleteEnvelopes(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "agentic_stream.normalized.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Frames []golden.Frame `json:"frames"`
	}
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{"TEXT_MESSAGE_CHUNK", "CUSTOM", "CUSTOM", "CUSTOM", "CUSTOM"}
	if got := golden.FrameTypes(fixture.Frames); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("types = %v, want %v", got, wantTypes)
	}
	wantKinds := []convert.AgenticEnvelopeKind{convert.EnvelopeContentBlock, convert.EnvelopeResponseMeta, convert.EnvelopePaused, convert.EnvelopeResumed}
	var gotKinds []convert.AgenticEnvelopeKind
	for _, frame := range fixture.Frames {
		if frame.Data["type"] != "CUSTOM" {
			continue
		}
		envelope, err := convert.DecodeAgenticEnvelope(frame.Data["value"])
		if err != nil {
			t.Fatalf("decode golden envelope: %v", err)
		}
		gotKinds = append(gotKinds, envelope.Kind)
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("kinds = %v, want %v", gotKinds, wantKinds)
	}
}

func TestAgenticConvertGoldenEnumeratesPublicShapes(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "agentic_convert.normalized.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ContentKinds []string `json:"contentKinds"`
		PublicShapes []struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		} `json:"publicShapes"`
		AnnotationShapes map[string][]string `json:"annotationShapes"`
		ResponseMeta     string              `json:"responseMetaEnvelope"`
	}
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.ContentKinds) != 20 || len(fixture.PublicShapes) != 20 {
		t.Fatalf("content kinds=%d public shapes=%d", len(fixture.ContentKinds), len(fixture.PublicShapes))
	}
	for i, shape := range fixture.PublicShapes {
		if shape.Type != fixture.ContentKinds[i] || shape.Payload == "" {
			t.Fatalf("shape %d = %#v for kind %q", i, shape, fixture.ContentKinds[i])
		}
	}
	if len(fixture.AnnotationShapes["openai"]) != 4 || len(fixture.AnnotationShapes["claude"]) != 4 || len(fixture.AnnotationShapes["gemini"]) != 4 {
		t.Fatalf("annotation shapes = %#v", fixture.AnnotationShapes)
	}
	if fixture.ResponseMeta != string(convert.EnvelopeResponseMeta) {
		t.Fatalf("response metadata envelope = %q", fixture.ResponseMeta)
	}
}
