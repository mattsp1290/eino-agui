package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	FrameIDPlaceholder   = "<sse-id>"
	MessageIDPlaceholder = "<message-id>"
	TimestampPlaceholder = "<timestamp>"
)

// Frame is a normalized SSE frame.
type Frame struct {
	ID    string         `json:"id,omitempty"`
	Event string         `json:"event,omitempty"`
	Data  map[string]any `json:"data"`
}

// NormalizeSSE parses SDK-formatted SSE bytes and masks runtime-minted IDs and
// timestamps.
func NormalizeSSE(raw []byte) ([]Frame, error) {
	chunks := bytes.Split(raw, []byte("\n\n"))
	frames := make([]Frame, 0, len(chunks))
	state := newNormalizationState()
	for _, chunk := range chunks {
		chunk = bytes.TrimSpace(chunk)
		if len(chunk) == 0 {
			continue
		}
		frame, err := parseFrame(chunk, state)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func parseFrame(chunk []byte, state *normalizationState) (Frame, error) {
	var frame Frame
	for _, line := range bytes.Split(chunk, []byte("\n")) {
		key, value, ok := strings.Cut(string(line), ":")
		if !ok {
			return Frame{}, fmt.Errorf("golden: malformed SSE line %q", line)
		}
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "id":
			frame.ID = FrameIDPlaceholder
		case "event":
			frame.Event = value
		case "data":
			var data map[string]any
			if err := json.Unmarshal([]byte(value), &data); err != nil {
				return Frame{}, fmt.Errorf("golden: decode data line: %w", err)
			}
			normalized := state.normalizeValue(data)
			var ok bool
			frame.Data, ok = normalized.(map[string]any)
			if !ok {
				return Frame{}, fmt.Errorf("golden: normalized data is %T, want map", normalized)
			}
		default:
			return Frame{}, fmt.Errorf("golden: unsupported SSE field %q", key)
		}
	}
	if frame.Data == nil {
		return Frame{}, fmt.Errorf("golden: frame missing data line")
	}
	return frame, nil
}

// NormalizeValue recursively masks runtime-minted values in decoded fixture data.
func NormalizeValue(value any) any {
	return newNormalizationState().normalizeValue(value)
}

type normalizationState struct {
	messageIDs map[string]string
}

func newNormalizationState() *normalizationState {
	return &normalizationState{messageIDs: make(map[string]string)}
}

func (s *normalizationState) normalizeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = s.normalizeField(key, child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = s.normalizeValue(v[i])
		}
		return out
	default:
		return value
	}
}

func (s *normalizationState) normalizeField(key string, value any) any {
	switch key {
	case "timestamp":
		return TimestampPlaceholder
	case "messageId", "parentMessageId":
		if shouldMaskString(value) {
			return s.messageID(value.(string))
		}
	case "id":
		if shouldMaskString(value) {
			return s.messageID(value.(string))
		}
	}
	return s.normalizeValue(value)
}

func (s *normalizationState) messageID(id string) string {
	if placeholder, ok := s.messageIDs[id]; ok {
		return placeholder
	}
	placeholder := MessageIDPlaceholder
	if len(s.messageIDs) > 0 {
		placeholder = fmt.Sprintf("<message-id-%d>", len(s.messageIDs)+1)
	}
	s.messageIDs[id] = placeholder
	return placeholder
}

func shouldMaskString(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(s, "msg-") ||
		strings.HasPrefix(s, "golden-msg-") ||
		strings.HasPrefix(s, "fixture-msg-") ||
		strings.Contains(s, "-msg-")
}

// FrameTypes returns the normalized event type sequence.
func FrameTypes(frames []Frame) []string {
	types := make([]string, 0, len(frames))
	for _, frame := range frames {
		if eventType, ok := frame.Data["type"].(string); ok {
			types = append(types, eventType)
		}
	}
	return types
}

// CountType returns the number of frames with the given AG-UI event type.
func CountType(frames []Frame, eventType string) int {
	count := 0
	for _, frame := range frames {
		if got, ok := frame.Data["type"].(string); ok && got == eventType {
			count++
		}
	}
	return count
}

// IntString formats i using base 10. It exists to keep fixture-generating tests
// from reaching for fmt when they only need a stable decimal token.
func IntString(i int) string {
	return strconv.Itoa(i)
}
