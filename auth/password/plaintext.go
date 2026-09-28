package password

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Plaintext is a bounded password input, never a persisted hash. Its JSON
// decoder preserves exact input bytes; output/logging redacts it. Creation does
// not enforce an application's password-strength policy or verify credentials.
type Plaintext struct{ value secret.String }

func NewPlaintext(raw secret.String) (Plaintext, error) {
	if raw.IsZero() || len(raw.Reveal()) > MaxPasswordBytes {
		return Plaintext{}, fault.New(fault.Invalid, "password input is empty or exceeds its byte limit")
	}
	return Plaintext{value: raw}, nil
}

// Secret explicitly exposes the input for domain strength validation or a
// credential adapter. The returned secret still redacts ordinary serialization.
func (p Plaintext) Secret() secret.String { return p.value }

func (p Plaintext) Validate() error            { _, err := NewPlaintext(p.value); return err }
func (Plaintext) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (Plaintext) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (Plaintext) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
func (p *Plaintext) UnmarshalJSON(data []byte) error {
	if p == nil {
		return fault.New(fault.Invalid, "password input destination is missing")
	}
	*p = Plaintext{}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fault.New(fault.Invalid, "invalid password input")
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
