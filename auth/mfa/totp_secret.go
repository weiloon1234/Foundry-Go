package mfa

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const totpSecretBytes = 20
const totpPeriodSeconds int64 = 30
const totpDigits = 6

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// TOTPSecret is a canonical unpadded uppercase base32 160-bit secret. Only Secret
// deliberately reveals it for encrypted persistence or authenticated enrollment
// delivery. Plain database storage and ordinary response serialization are not
// supported. This type does not promise memory erasure.
type TOTPSecret struct{ encoded secret.String }

func ParseTOTPSecret(encoded secret.String) (TOTPSecret, error) {
	if len(encoded.Reveal()) != totpEncoding.EncodedLen(totpSecretBytes) {
		return TOTPSecret{}, fault.New(fault.Invalid, "invalid TOTP secret")
	}
	raw, err := totpEncoding.DecodeString(encoded.Reveal())
	if err != nil || len(raw) != totpSecretBytes || totpEncoding.EncodeToString(raw) != encoded.Reveal() {
		return TOTPSecret{}, fault.New(fault.Invalid, "invalid TOTP secret encoding")
	}
	return TOTPSecret{encoded: encoded}, nil
}
func GenerateTOTPSecret() (TOTPSecret, error) {
	var raw [totpSecretBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return TOTPSecret{}, fault.Wrap(fault.Internal, "TOTP secret generation failed", err)
	}
	return ParseTOTPSecret(secret.New(totpEncoding.EncodeToString(raw[:])))
}
func (s TOTPSecret) Secret() secret.String      { return s.encoded }
func (s TOTPSecret) Validate() error            { _, err := ParseTOTPSecret(s.encoded); return err }
func (TOTPSecret) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (TOTPSecret) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (TOTPSecret) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
