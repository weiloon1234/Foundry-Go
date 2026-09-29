package record

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// State distinguishes missing model snapshots, disclosed SQL values, completely
// redacted values and JSON whose sensitive descendants have been redacted.
// SQL NULL is a disclosed SQL value, distinct from an absent model snapshot.
// Oversized marks a value that exceeded the audit value bound; its Digest
// retains the stored size and, when safe, a SHA-256 of the captured bytes.
type State uint8

const (
	Absent State = iota
	Disclosed
	Redacted
	RedactedJSON
	Oversized
)

// Digest describes an oversized value without retaining it. Size is the number
// of captured bytes: UTF-8 text, raw bytes or the already-redacted canonical
// JSON document. SHA256 is the lowercase hexadecimal digest of those bytes; it
// is empty for JSON that was too large to inspect for sensitive keys, so a
// digest never allows confirming a guessed secret.
type Digest struct {
	Size   int64
	SHA256 string
}

// Snapshot owns one immutable audit value. It is an audit representation, not a
// model field or response DTO. Format omits content; JSON is an explicit export.
type Snapshot struct {
	state  State
	value  sqlvalue.Value
	digest Digest
}

func (s Snapshot) State() State                 { return s.state }
func (Snapshot) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit snapshot")) }

// Digest returns the size marker of an Oversized snapshot.
func (s Snapshot) Digest() (Digest, bool) {
	if s.state != Oversized {
		return Digest{}, false
	}
	return s.digest, true
}

type snapshotWire struct {
	State  State                          `json:"state"`
	Value  value.Optional[sqlvalue.Value] `json:"value,omitzero"`
	Size   value.Optional[int64]          `json:"size,omitzero"`
	SHA256 string                         `json:"sha256,omitzero"`
}

func (s Snapshot) wire() snapshotWire {
	wire := snapshotWire{State: s.state}
	switch s.state {
	case Disclosed, RedactedJSON:
		wire.Value = value.Set(s.value)
	case Oversized:
		wire.Size = value.Set(s.digest.Size)
		wire.SHA256 = s.digest.SHA256
	}
	return wire
}

func snapshotFromWire(wire snapshotWire) (Snapshot, error) {
	encoded, present := wire.Value.Get()
	if present != (wire.State == Disclosed || wire.State == RedactedJSON) {
		return Snapshot{}, invalidSnapshot()
	}
	size, sized := wire.Size.Get()
	if sized != (wire.State == Oversized) || wire.SHA256 != "" && wire.State != Oversized {
		return Snapshot{}, invalidSnapshot()
	}
	result := Snapshot{state: wire.State, value: encoded, digest: Digest{Size: size, SHA256: wire.SHA256}}
	if err := result.validate(); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

// validate checks the policy-independent structure of one snapshot. Sensitive
// name and JSON-key checks depend on the row's Redaction and are applied by the
// containing record.
func (s Snapshot) validate() error {
	switch s.state {
	case Absent, Redacted:
		if s.value != (sqlvalue.Value{}) || s.digest != (Digest{}) {
			return invalidSnapshot()
		}
	case Disclosed, RedactedJSON:
		if s.digest != (Digest{}) || len(s.value.Text) > jsonwire.MaxBytes {
			return invalidSnapshot()
		}
		decoded, err := s.value.Decode()
		if err != nil {
			return invalidSnapshot()
		}
		canonical, err := sqlvalue.Encode(decoded)
		if err != nil || canonical != s.value {
			return invalidSnapshot()
		}
		if s.state == RedactedJSON && s.value.Kind != "string" {
			return invalidSnapshot()
		}
	case Oversized:
		if s.value != (sqlvalue.Value{}) || s.digest.Size < 1 || !validDigestText(s.digest.SHA256) {
			return invalidSnapshot()
		}
	default:
		return invalidSnapshot()
	}
	return nil
}

func validDigestText(text string) bool {
	if text == "" {
		return true
	}
	if len(text) != sha256.Size*2 {
		return false
	}
	for _, r := range text {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// oversized replaces a captured value with its size marker. Hashing covers the
// natural bytes of the stored representation, never a presentation value.
func oversized(data []byte, hashed bool) Snapshot {
	digest := Digest{Size: int64(len(data))}
	if hashed {
		sum := sha256.Sum256(data)
		digest.SHA256 = hex.EncodeToString(sum[:])
	}
	return Snapshot{state: Oversized, digest: digest}
}

// payloadBytes returns the natural byte length of a disclosed or JSON-redacted
// value: UTF-8 text/JSON bytes, raw binary bytes or the canonical scalar text.
func (s Snapshot) payloadBytes() ([]byte, error) {
	if s.value.Kind == "bytes" {
		return base64.RawURLEncoding.DecodeString(s.value.Text)
	}
	return []byte(s.value.Text), nil
}

// compact replaces a disclosed/JSON-redacted value above limit bytes with its
// hashed size marker. Other states are returned unchanged.
func (s Snapshot) compact(limit int) (Snapshot, bool, error) {
	if s.state != Disclosed && s.state != RedactedJSON || len(s.value.Text) <= limit {
		return s, false, nil
	}
	data, err := s.payloadBytes()
	if err != nil {
		return Snapshot{}, false, invalidSnapshot()
	}
	if len(data) <= limit {
		return s, false, nil
	}
	return oversized(data, true), true, nil
}

func invalidSnapshot() error { return fault.New(fault.Invalid, "invalid audit snapshot") }

func (s Snapshot) MarshalJSON() ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(s.wire())
}

func (s *Snapshot) UnmarshalJSON(data []byte) error {
	if s == nil {
		return invalidSnapshot()
	}
	parsed, err := value.ParseJSON[snapshotWire](string(data))
	if err != nil {
		return invalidSnapshot()
	}
	wire, err := parsed.Decode()
	if err != nil {
		return invalidSnapshot()
	}
	result, err := snapshotFromWire(wire)
	if err != nil {
		return err
	}
	*s = result
	return nil
}
