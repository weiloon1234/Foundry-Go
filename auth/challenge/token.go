package challenge

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Token retains its model and recovery purpose. Parsing validates syntax only;
// storage and current model state decide whether it may be consumed. Ordinary
// JSON/logging redacts it. Secret is the explicit delivery/transport boundary.
type Token[M any, P Purpose] struct {
	_     [0]*M
	_     [0]P
	value secret.String
}

// ParseToken validates token syntax without verifying storage, expiry or current model
// state.
func ParseToken[M any, P Purpose](raw secret.String) (Token[M, P], error) {
	if _, err := HashSecret(raw); err != nil {
		return Token[M, P]{}, err
	}
	return Token[M, P]{value: raw}, nil
}

// Secret explicitly exposes the secret wrapper for delivery or transport.
func (t Token[M, P]) Secret() secret.String { return t.value }

// Validate checks secret syntax without verifying challenge validity.
func (t Token[M, P]) Validate() error { _, err := HashSecret(t.value); return err }

// Format writes the redaction marker without exposing the value.
func (Token[M, P]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(secret.Redacted)) }

// LogValue returns the redaction marker without exposing the value.
func (Token[M, P]) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// MarshalJSON encodes the redaction marker without exposing the value.
func (Token[M, P]) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

// Digest is a nominal challenge hash, never interchangeable with access tokens.
type Digest struct{ hash credential.Digest }

// Format writes the label "challenge digest" without exposing the value.
func (Digest) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("challenge digest")) }

// Hex returns the digest's hexadecimal persistence representation.
func (d Digest) Hex() string { return d.hash.Hex() }

// IsZero reports whether the digest is uninitialized.
func (d Digest) IsZero() bool { return d.hash.IsZero() }

// Equal compares challenge digests.
func (d Digest) Equal(other Digest) bool { return d.hash.Equal(other.hash) }

// ParseDigest restores a challenge digest from its hexadecimal representation.
func ParseDigest(text string) (Digest, error) {
	hash, err := credential.ParseDigest(text)
	return Digest{hash: hash}, err
}

// HashSecret validates and hashes a challenge secret, returning Unauthenticated for
// malformed input.
func HashSecret(raw secret.String) (Digest, error) {
	hash, err := credential.Hash(raw)
	if err != nil {
		return Digest{}, auth.Unauthenticated.WithCause(nil)
	}
	return Digest{hash: hash}, nil
}
