package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const MaxKeys = 32
const keyBytes = 32
const maxEncryptions uint64 = 1 << 32

// KeyID is a non-secret stable name persisted in encrypted envelopes. Never
// change the material assigned to an existing ID.
type KeyID string

// Key owns one immutable AES-256 key and a local encryption budget shared by
// copies and keyrings. Loading the same material again or on another process
// creates a separate counter: operators must rotate before aggregate use across
// all instances/restarts exceeds 2^32 encryptions with the same material.
type Key struct{ state *keyState }
type keyState struct {
	id          KeyID
	material    [keyBytes]byte
	aead        cipher.AEAD
	encryptions atomic.Uint64
}

// ParseKey accepts exactly 32 bytes encoded as canonical unpadded base64url.
// It performs no I/O and never logs or includes key material in errors.
func ParseKey(id KeyID, encoded secret.String) (Key, error) {
	if !identifier.Semantic(string(id)) || len(encoded.Reveal()) != base64.RawURLEncoding.EncodedLen(keyBytes) {
		return Key{}, fault.New(fault.Invalid, "invalid encryption key")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded.Reveal())
	if err != nil || len(raw) != keyBytes || base64.RawURLEncoding.EncodeToString(raw) != encoded.Reveal() {
		return Key{}, fault.New(fault.Invalid, "invalid encryption key encoding")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return Key{}, fault.Wrap(fault.Internal, "encryption initialization failed", err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return Key{}, fault.Wrap(fault.Internal, "encryption initialization failed", err)
	}
	state := &keyState{id: id, aead: aead}
	copy(state.material[:], raw)
	return Key{state}, nil
}

// GenerateKey creates a key for explicit installation in a secret store. Persist
// its Secret before encrypting durable data; do not generate keys at each boot.
func GenerateKey(id KeyID) (Key, error) {
	if !identifier.Semantic(string(id)) {
		return Key{}, fault.New(fault.Invalid, "invalid encryption key ID")
	}
	var raw [keyBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Key{}, fault.Wrap(fault.Internal, "encryption key generation failed", err)
	}
	return ParseKey(id, secret.New(base64.RawURLEncoding.EncodeToString(raw[:])))
}
func (k Key) ID() KeyID {
	if k.state == nil {
		return ""
	}
	return k.state.id
}
func (k Key) Secret() secret.String {
	if k.state == nil {
		return secret.String{}
	}
	return secret.New(base64.RawURLEncoding.EncodeToString(k.state.material[:]))
}
func (Key) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Key) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Key) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

// Keyring encrypts with one active key and decrypts with its retained keys. It
// is immutable and safe for concurrent use. Retain old keys until every record
// has been re-encrypted; unknown IDs never fall back to the active key.
type Keyring struct {
	active *keyState
	keys   map[KeyID]*keyState
}

func NewKeyring(active KeyID, keys ...Key) (*Keyring, error) {
	if len(keys) < 1 || len(keys) > MaxKeys {
		return nil, fault.New(fault.Invalid, "encryption requires a bounded nonempty keyring")
	}
	ring := &Keyring{keys: make(map[KeyID]*keyState, len(keys))}
	for _, key := range keys {
		if key.state == nil {
			return nil, fault.New(fault.Invalid, "encryption key is uninitialized")
		}
		if _, exists := ring.keys[key.ID()]; exists {
			return nil, fault.New(fault.Duplicate, "duplicate encryption key ID")
		}
		for _, prior := range ring.keys {
			if subtle.ConstantTimeCompare(prior.material[:], key.state.material[:]) == 1 {
				return nil, fault.New(fault.Duplicate, "encryption key material is reused")
			}
		}
		ring.keys[key.ID()] = key.state
	}
	ring.active = ring.keys[active]
	if ring.active == nil {
		return nil, fault.New(fault.Invalid, "active encryption key is missing")
	}
	return ring, nil
}
func (r *Keyring) Validate() error {
	if r == nil || r.active == nil {
		return fault.New(fault.Invalid, "encryption keyring is uninitialized")
	}
	return nil
}
func (r *Keyring) ActiveID() KeyID {
	if r == nil || r.active == nil {
		return ""
	}
	return r.active.id
}
func (Keyring) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("encryption keyring")) }
func (Keyring) LogValue() slog.Value         { return slog.StringValue("encryption keyring") }
func (Keyring) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

func (k *keyState) reserve() error {
	for {
		count := k.encryptions.Load()
		if count >= maxEncryptions {
			return fault.New(fault.Internal, "encryption key usage limit reached; rotate the key")
		}
		if k.encryptions.CompareAndSwap(count, count+1) {
			return nil
		}
	}
}
