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
	candidate := secret.New(raw)
	if _, err := token.HashSecret(candidate); err != nil {
		return secret.String{}, fault.New(fault.Invalid, "invalid token credential")
	}
	return candidate, nil
}
