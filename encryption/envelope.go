package encryption

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const MaxPlaintextBytes = 1 << 20
const version = "fg1"
const overhead = 28 // Standard random-nonce GCM: 12 nonce bytes + 16 tag bytes.
const maxEnvelopeBytes = len(version) + 1 + 128 + 1 + (MaxPlaintextBytes+overhead+2)/3*4

// Ciphertext is a bounded, canonical versioned envelope. Parsing validates its
// structure only; authentication happens in Keyring.Decrypt. Encoded is the
// explicit persistence boundary. Ordinary formatting and JSON redact it.
type Ciphertext struct {
	encoded string
	id      KeyID
}

func ParseCiphertext(encoded string) (Ciphertext, error) {
	invalid := func() (Ciphertext, error) {
		return Ciphertext{}, fault.New(fault.Invalid, "invalid encryption envelope")
	}
	if len(encoded) > maxEnvelopeBytes {
		return invalid()
	}
	parts := strings.SplitN(encoded, ":", 3)
	if len(parts) != 3 || parts[0] != version || !identifier.Semantic(parts[1]) {
		return invalid()
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(raw) < overhead || len(raw) > MaxPlaintextBytes+overhead || base64.RawURLEncoding.EncodeToString(raw) != parts[2] {
		return invalid()
	}
	return Ciphertext{encoded: encoded, id: KeyID(parts[1])}, nil
}

// EnvelopePrefix returns the text every envelope encrypted under id starts
// with. Storage adapters use it to select records that still use another key
// during rotation; it is not secret and never authenticates anything.
func EnvelopePrefix(id KeyID) (string, error) {
	if !identifier.Semantic(string(id)) {
		return "", fault.New(fault.Invalid, "invalid encryption key ID")
	}
	return version + ":" + string(id) + ":", nil
}

func (c Ciphertext) Encoded() string            { return c.encoded }
func (c Ciphertext) KeyID() KeyID               { return c.id }
func (c Ciphertext) IsZero() bool               { return c.encoded == "" }
func (Ciphertext) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Ciphertext) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Ciphertext) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
