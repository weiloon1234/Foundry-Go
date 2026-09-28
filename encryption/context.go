package encryption

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const MaxBindingBytes = 8192

// Purpose separates uses of encryption, for example auth.mfa.totp.v1.
type Purpose string

// Context authenticates a purpose and exact owning-record binding alongside an
// envelope. It is not stored in the envelope and must be reconstructed from
// trusted record identity at decryption. Include tenant and record generation
// when relevant. Empty bindings are rejected.
type Context struct {
	purpose Purpose
	binding string
}

func NewContext(purpose Purpose, binding secret.String) (Context, error) {
	if !identifier.Semantic(string(purpose)) || binding.IsZero() || len(binding.Reveal()) > MaxBindingBytes {
		return Context{}, fault.New(fault.Invalid, "invalid encryption context")
	}
	return Context{purpose: purpose, binding: binding.Reveal()}, nil
}
func (c Context) Validate() error            { _, err := NewContext(c.purpose, secret.New(c.binding)); return err }
func (Context) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("encryption context")) }
func (Context) LogValue() slog.Value         { return slog.StringValue("encryption context") }
func (Context) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
func (c Context) aad(id KeyID) []byte {
	// Length prefixes preserve exact boundaries, including arbitrary binary
	// bindings that must not undergo UTF-8 replacement or normalization.
	return appendContext(nil, version, string(id), string(c.purpose), c.binding)
}
func appendContext(out []byte, parts ...string) []byte {
	for _, part := range parts {
		size := uint32(len(part))
		out = binary.BigEndian.AppendUint32(out, size)
		out = append(out, part...)
	}
	return out
}
