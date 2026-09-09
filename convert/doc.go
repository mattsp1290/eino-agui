// Package convert maps AG-UI protocol messages to eino schema messages and
// back. Protocol-only message and tool-call metadata, subagent attribution,
// and encrypted continuity are carried in a package-owned Eino Extra envelope.
// ToAGUITokenUsage maps classic Eino usage without inventing absent counts.
//
// The package deliberately keeps route/provider policy outside the conversion
// code. Callers decide whether multimodal image input is allowed by passing
// WithVisionSupport to ToEinoMessages.
package convert
