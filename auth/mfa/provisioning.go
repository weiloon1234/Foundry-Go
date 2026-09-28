package mfa

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// ProvisioningURI constructs the otpauth URI for an authenticated enrollment
// response or local QR renderer. The URI contains the TOTP secret; never pass
// it to a remote QR-generation service, analytics or logging. Issuer/account
// labels must be nonempty, bounded, colon-free UTF-8 without control characters.
func ProvisioningURI(key TOTPSecret, issuer, account string) (secret.String, error) {
	if err := key.Validate(); err != nil {
		return secret.String{}, err
	}
	if !validLabel(issuer) || !validLabel(account) {
		return secret.String{}, fault.New(fault.Invalid, "invalid TOTP issuer or account label")
	}
	label := issuer + ":" + account
	uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + label, RawPath: "/" + url.PathEscape(label)}
	query := url.Values{"secret": {key.Secret().Reveal()}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {strconv.Itoa(totpDigits)}, "period": {strconv.FormatInt(totpPeriodSeconds, 10)}}
	uri.RawQuery = query.Encode()
	return secret.New(uri.String()), nil
}
func validLabel(text string) bool {
	if len(text) == 0 || len(text) > 256 || !utf8.ValidString(text) || strings.TrimSpace(text) != text || strings.ContainsRune(text, ':') {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
