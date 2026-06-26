// Package convert maps AG-UI protocol messages to eino schema messages and
// back.
//
// The package deliberately keeps route/provider policy outside the conversion
// code. Callers decide whether multimodal image input is allowed by passing
// WithVisionSupport to ToEinoMessages.
package convert
