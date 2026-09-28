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

func ParseToken[M any, P Purpose](raw secret.String) (Token[M, P], error) {
	if _, err := HashSecret(raw); err != nil {
		return Token[M, P]{}, err
	}
	return Token[M, P]{value: raw}, nil
}
func (t Token[M, P]) Secret() secret.String      { return t.value }
func (t Token[M, P]) Validate() error            { _, err := HashSecret(t.value); return err }
func (Token[M, P]) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Token[M, P]) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Token[M, P]) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

// Digest is a nominal challenge hash, never interchangeable with access tokens.
type Digest struct{ hash credential.Digest }

func (Digest) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("challenge digest")) }
func (d Digest) Hex() string              { return d.hash.Hex() }
func (d Digest) IsZero() bool             { return d.hash.IsZero() }
func (d Digest) Equal(other Digest) bool  { return d.hash.Equal(other.hash) }
func ParseDigest(text string) (Digest, error) {
	hash, err := credential.ParseDigest(text)
	return Digest{hash: hash}, err
}
func HashSecret(raw secret.String) (Digest, error) {
	hash, err := credential.Hash(raw)
	if err != nil {
		return Digest{}, auth.Unauthenticated
	}
	return Digest{hash: hash}, nil
}
