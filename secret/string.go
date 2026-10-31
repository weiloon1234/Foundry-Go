// Package secret makes accidental formatting/serialization of credentials safe.
// It does not promise memory erasure or protect values explicitly revealed by
// application code for use with an external client.
package secret

import (
	"encoding/json"
	"log/slog"
)

const Redacted = "[REDACTED]"

// String holds a sensitive string. Only Reveal returns the original value.
type String struct{ value string }

// New wraps a string as a secret.
func New(value string) String { return String{value: value} }

// Reveal returns the underlying unredacted string.
func (s String) Reveal() string { return s.value }

// IsZero reports whether the underlying secret is empty.
func (s String) IsZero() bool { return s.value == "" }

// String returns the redaction marker.
func (s String) String() string { return Redacted }

// GoString returns the redaction marker.
func (s String) GoString() string { return Redacted }

// LogValue returns the redaction marker for logging.
func (s String) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalText encodes the redaction marker as text.
func (s String) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

// MarshalJSON encodes the redaction marker as JSON.
func (s String) MarshalJSON() ([]byte, error) { return json.Marshal(Redacted) }

// UnmarshalText replaces the secret with the supplied text.
func (s *String) UnmarshalText(data []byte) error { s.value = string(data); return nil }
