package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// SigningKeyID is the public rotation identifier, not secret key material.
type SigningKeyID string

// SigningKey pairs a public identifier with explicitly protected key material.
// Use cryptographically random material. Length validation cannot prove entropy.
type SigningKey struct {
	ID     SigningKeyID
	Secret secret.String
}

func (k SigningKey) String() string   { return secret.Redacted }
func (k SigningKey) GoString() string { return secret.Redacted }

// SigningKeys is an immutable rotation set. Active signs new values; previous
// keys only verify. Removing a key invalidates signatures created with that key.
type SigningKeys struct {
	active SigningKeyID
	keys   map[SigningKeyID]secret.String
}

func (k SigningKeys) String() string   { return secret.Redacted }
func (k SigningKeys) GoString() string { return secret.Redacted }

// NewSigningKeys copies at most sixteen distinct keys. IDs are bounded ASCII
// letters, digits, underscores or hyphens. Material is 32–1024 bytes. Neither keys
// nor their signatures are inferred from an application name or environment.
func NewSigningKeys(active SigningKey, previous ...SigningKey) (SigningKeys, error) {
	if len(previous) > 15 {
		return SigningKeys{}, fault.New(fault.Invalid, "too many signing keys")
	}
	result := SigningKeys{active: active.ID, keys: make(map[SigningKeyID]secret.String, len(previous)+1)}
	add := func(k SigningKey) error {
		if !validSigningKeyID(k.ID) || len(k.Secret.Reveal()) < 32 || len(k.Secret.Reveal()) > 1024 {
			return fault.New(fault.Invalid, "invalid signing key identifier or material length")
		}
		if _, ok := result.keys[k.ID]; ok {
			return fault.New(fault.Duplicate, "signing key identifier is repeated")
		}
		result.keys[k.ID] = k.Secret
		return nil
	}
	if err := add(active); err != nil {
		return SigningKeys{}, err
	}
	for _, key := range previous {
		if err := add(key); err != nil {
			return SigningKeys{}, err
		}
	}
	return result, nil
}
func (k SigningKeys) ActiveID() SigningKeyID { return k.active }
func (k SigningKeys) Validate() error {
	if _, ok := k.keys[k.active]; !ok {
		return fault.New(fault.Invalid, "signing keys have no active key")
	}
	return nil
}
func validSigningKeyID(id SigningKeyID) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := range len(id) {
		b := id[i]
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-') {
			return false
		}
	}
	return true
}

// Purpose framing is shared by cookie and future signed-route adapters.
func signingMAC(key secret.String, purpose, content string) []byte {
	mac := hmac.New(sha256.New, []byte(key.Reveal()))
	fmt.Fprintf(mac, "%d:", len(purpose))
	mac.Write([]byte(purpose))
	mac.Write([]byte(content))
	return mac.Sum(nil)
}
func (k SigningKeys) sign(purpose, content string) string {
	return base64.RawURLEncoding.EncodeToString(signingMAC(k.keys[k.active], purpose, content))
}
func (k SigningKeys) verify(id SigningKeyID, purpose, content, tag string) bool {
	key, ok := k.keys[id]
	if !ok {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(tag)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != tag {
		return false
	}
	return hmac.Equal(decoded, signingMAC(key, purpose, content))
}
