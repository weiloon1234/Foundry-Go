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

func New(value string) String                     { return String{value: value} }
func (s String) Reveal() string                   { return s.value }
func (s String) IsZero() bool                     { return s.value == "" }
func (s String) String() string                   { return Redacted }
func (s String) GoString() string                 { return Redacted }
func (s String) LogValue() slog.Value             { return slog.StringValue(Redacted) }
func (s String) MarshalText() ([]byte, error)     { return []byte(Redacted), nil }
func (s String) MarshalJSON() ([]byte, error)     { return json.Marshal(Redacted) }
func (s *String) UnmarshalText(data []byte) error { s.value = string(data); return nil }
