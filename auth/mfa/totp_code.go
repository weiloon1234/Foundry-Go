package mfa

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// TOTPCode preserves six ASCII digits, including leading zeroes. It is a typed
// unverified input. No trimming, Unicode digit conversion or truncation occurs.
type TOTPCode struct{ digits secret.String }

// ParseTOTPCode requires exactly six ASCII digits without checking their cryptographic
// validity.
func ParseTOTPCode(raw secret.String) (TOTPCode, error) {
	text := raw.Reveal()
	if len(text) != totpDigits {
		return TOTPCode{}, safeerror.NewFault(fault.Invalid, "TOTP code must contain six ASCII digits")
	}
	for i := range text {
		if text[i] < '0' || text[i] > '9' {
			return TOTPCode{}, safeerror.NewFault(fault.Invalid, "TOTP code must contain six ASCII digits")
		}
	}
	return TOTPCode{digits: raw}, nil
}

// Secret explicitly exposes the six-digit code wrapper.
func (c TOTPCode) Secret() secret.String { return c.digits }

// Validate checks six-digit syntax without verifying the code against a factor.
func (c TOTPCode) Validate() error { _, err := ParseTOTPCode(c.digits); return err }

// Format writes the redaction marker without exposing the value.
func (TOTPCode) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(secret.Redacted)) }

// LogValue returns the redaction marker without exposing the value.
func (TOTPCode) LogValue() slog.Value { return slog.StringValue(secret.Redacted) }

// MarshalJSON encodes the redaction marker without exposing the value.
func (TOTPCode) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

// UnmarshalJSON clears the receiver and parses a bounded JSON string as six ASCII
// digits.
func (c *TOTPCode) UnmarshalJSON(data []byte) error {
	if c == nil {
		return safeerror.NewFault(fault.Invalid, "TOTP code destination is missing")
	}
	*c = TOTPCode{}
	var raw string
	if len(data) > 128 {
		return safeerror.NewFault(fault.Invalid, "invalid TOTP code input")
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return safeerror.NewFault(fault.Invalid, "invalid TOTP code input")
	}
	parsed, err := ParseTOTPCode(secret.New(raw))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// JSONContract describes TOTP input as a JSON string; ordinary output remains redacted.
func (TOTPCode) JSONContract() contract.JSON[TOTPCode] {
	const id contract.TypeID = "github.com/weiloon1234/Foundry-Go/auth/mfa.TOTPCode"
	return contract.DefineJSONValue[TOTPCode](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}
