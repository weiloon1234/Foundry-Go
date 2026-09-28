package idempotenthttp

import (
	"foundry.test/consumer/internal/authfixture"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/http"
)

// Actor remains the concrete shared fixture identity outside public wire DTOs.
type Actor = authfixture.Actor

// Authentication uses the shared loopback credentials, never deployment secrets.
func Authentication() (*http.Authentication, auth.Guard[Actor], error) {
	return authfixture.Authentication()
}
