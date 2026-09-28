package mfa

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// TOTPCode preserves six ASCII digits, including leading zeroes. It is a typed
// unverified input. No trimming, Unicode digit conversion or truncation occurs.
type TOTPCode struct{ digits secret.String }

func ParseTOTPCode(raw secret.String) (TOTPCode, error) {
	text := raw.Reveal()
	if len(text) != totpDigits {
		return TOTPCode{}, fault.New(fault.Invalid, "TOTP code must contain six ASCII digits")
	}
	for i := range text {
		if text[i] < '0' || text[i] > '9' {
			return TOTPCode{}, fault.New(fault.Invalid, "TOTP code must contain six ASCII digits")
		}
	}
	return TOTPCode{digits: raw}, nil
}
func (c TOTPCode) Secret() secret.String      { return c.digits }
func (c TOTPCode) Validate() error            { _, err := ParseTOTPCode(c.digits); return err }
func (TOTPCode) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (TOTPCode) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (TOTPCode) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
func (c *TOTPCode) UnmarshalJSON(data []byte) error {
	if c == nil {
		return fault.New(fault.Invalid, "TOTP code destination is missing")
	}
	*c = TOTPCode{}
	var raw string
	if len(data) > 128 {
		return fault.New(fault.Invalid, "invalid TOTP code input")
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fault.New(fault.Invalid, "invalid TOTP code input")
	}
	parsed, err := ParseTOTPCode(secret.New(raw))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
func (TOTPCode) JSONContract() contract.JSON[TOTPCode] {
	const id contract.TypeID = "github.com/weiloon1234/Foundry-Go/auth/mfa.TOTPCode"
	return contract.DefineJSONValue[TOTPCode](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}
