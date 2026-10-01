package authtransport

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Both input roles use the token runtime's canonical syntax. The nominal input
// wrappers stay distinct; parsing never authenticates or decides assurance.
func parseTokenCredential(data []byte) (secret.String, error) {
	if len(data) > 512 {
		return secret.String{}, fault.New(fault.Invalid, "invalid token credential")
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return secret.String{}, fault.New(fault.Invalid, "invalid token credential")
	}
	return checkTokenCredential(secret.New(raw))
}

// checkTokenCredential applies the canonical syntax to a credential from any
// transport, so a cookie and a JSON field accept exactly the same values.
func checkTokenCredential(candidate secret.String) (secret.String, error) {
	if len(candidate.Reveal()) > 512 {
		return secret.String{}, fault.New(fault.Invalid, "invalid token credential")
	}
	if _, err := token.HashSecret(candidate); err != nil {
		return secret.String{}, fault.New(fault.Invalid, "invalid token credential")
	}
	return candidate, nil
}

// NewRefreshCredential validates a refresh secret read from a transport other
// than JSON, such as an HttpOnly cookie.
func NewRefreshCredential(raw secret.String) (RefreshCredential, error) {
	candidate, err := checkTokenCredential(raw)
	if err != nil {
		return RefreshCredential{}, err
	}
	return RefreshCredential{secret: candidate}, nil
}
