package token

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Digest is a token-store hash. It is distinct from a session digest and cannot
// substitute for an opaque credential supplied at a transport boundary.
type Digest struct{ hash credential.Digest }

func (Digest) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("token digest")) }
func (d Digest) Hex() string              { return d.hash.Hex() }
func (d Digest) IsZero() bool             { return d.hash.IsZero() }
func (d Digest) Equal(other Digest) bool  { return d.hash.Equal(other.hash) }
func ParseDigest(text string) (Digest, error) {
	hash, err := credential.ParseDigest(text)
	return Digest{hash: hash}, err
}
func HashSecret(value secret.String) (Digest, error) {
	hash, err := credential.Hash(value)
	if err != nil {
		return Digest{}, auth.Unauthenticated
	}
	return Digest{hash: hash}, nil
}
func newSecret() (secret.String, Digest, error) {
	value, hash, err := credential.New()
	return value, Digest{hash: hash}, err
}
