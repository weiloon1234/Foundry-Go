// Package credential owns private primitives shared by stored authentication
// credentials. Public packages preserve their own typed persistence boundaries.
package credential

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/randomtoken"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const SecretBytes = 32
const EncodedSecretBytes = 43

type Digest struct{ bytes [sha256.Size]byte }

func (d Digest) Hex() string  { return hex.EncodeToString(d.bytes[:]) }
func (d Digest) IsZero() bool { return d == Digest{} }
func (d Digest) Equal(other Digest) bool {
	return subtle.ConstantTimeCompare(d.bytes[:], other.bytes[:]) == 1
}
func ParseDigest(text string) (Digest, error) {
	var result Digest
	if len(text) != sha256.Size*2 {
		return result, fault.New(fault.Invalid, "invalid stored credential digest")
	}
	if _, err := hex.Decode(result.bytes[:], []byte(text)); err != nil || result.Hex() != text || result.IsZero() {
		return Digest{}, fault.New(fault.Invalid, "invalid stored credential digest")
	}
	return result, nil
}
func Hash(value secret.String) (Digest, error) {
	text := value.Reveal()
	if len(text) != EncodedSecretBytes {
		return Digest{}, fault.New(fault.Invalid, "invalid credential encoding")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || len(decoded) != SecretBytes || base64.RawURLEncoding.EncodeToString(decoded) != text {
		return Digest{}, fault.New(fault.Invalid, "invalid credential encoding")
	}
	return Digest{bytes: sha256.Sum256(decoded)}, nil
}
func New() (secret.String, Digest, error) {
	token, err := randomtoken.Base64(SecretBytes)
	if err != nil {
		return secret.String{}, Digest{}, fault.Wrap(fault.Internal, "credential generation failed", err)
	}
	digest, err := Hash(token)
	if err != nil {
		return secret.String{}, Digest{}, err
	}
	return token, digest, nil
}
