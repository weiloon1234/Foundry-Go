package password

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Plaintext is a bounded password input, never a persisted hash. Its JSON
// decoder preserves exact input bytes; output/logging redacts it. Creation does
// not enforce an application's password-strength policy or verify credentials.
type Plaintext struct{ value secret.String }

// NewPlaintext requires nonempty password input no longer than MaxPasswordBytes.
func NewPlaintext(raw secret.String) (Plaintext, error) {
	if raw.IsZero() || len(raw.Reveal()) > MaxPasswordBytes {
		return Plaintext{}, safeerror.NewFault(fault.Invalid, "password input is empty or exceeds its byte limit")
	}
	return Plaintext{value: raw}, nil
}

// Secret explicitly exposes the input for domain strength validation or a
// credential adapter. The returned secret still redacts ordinary serialization.
func (p Plaintext) Secret() secret.String { return p.value }

// Validate checks the password's nonempty input and byte bound.
func (p Plaintext) Validate() error { _, err := NewPlaintext(p.value); return err }

// Format writes the redaction marker without exposing the value.
func (Plaintext) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(secret.Redacted)) }

// LogValue returns the redaction marker without exposing the value.
func (Plaintext) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// MarshalJSON encodes the redaction marker without exposing the value.
func (Plaintext) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

// UnmarshalJSON clears the receiver and decodes a nonempty, bounded password string.
func (p *Plaintext) UnmarshalJSON(data []byte) error {
	if p == nil {
		return safeerror.NewFault(fault.Invalid, "password input destination is missing")
	}
	*p = Plaintext{}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return safeerror.NewFault(fault.Invalid, "invalid password input")
	}
	parsed, err := NewPlaintext(secret.New(raw))
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// JSONContract allows a generated login/request DTO to discover this input's
// wire string shape without exposing the private Go representation.
func (Plaintext) JSONContract() contract.JSON[Plaintext] {
	const id contract.TypeID = "github.com/weiloon1234/Foundry-Go/auth/password.Plaintext"
	return contract.DefineJSONValue[Plaintext](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}
