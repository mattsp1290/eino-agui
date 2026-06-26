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
	for _, chunk := range chunks {
		chunk = bytes.TrimSpace(chunk)
		if len(chunk) == 0 {
			continue
		}
		frame, err := parseFrame(chunk)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func parseFrame(chunk []byte) (Frame, error) {
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
			normalized := NormalizeValue(data)
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
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = normalizeField(key, child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = NormalizeValue(v[i])
		}
		return out
	default:
		return value
	}
}

func normalizeField(key string, value any) any {
	switch key {
	case "timestamp":
		return TimestampPlaceholder
	case "messageId":
		if shouldMaskString(value) {
			return MessageIDPlaceholder
		}
	case "id":
		if shouldMaskString(value) {
			return MessageIDPlaceholder
		}
	}
	return NormalizeValue(value)
}

func shouldMaskString(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(s, "msg-") ||
		strings.HasPrefix(s, "golden-msg-") ||
		strings.HasPrefix(s, "fixture-msg-")
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
