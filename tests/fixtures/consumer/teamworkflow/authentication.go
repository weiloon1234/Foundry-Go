package teamworkflow

import (
	"foundry.test/consumer/internal/authfixture"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Actor reuses the shared loopback identity without coupling application fixtures.
type Actor = authfixture.Actor

// captureGuard binds the fixture's loopback authentication to the capture viewer gate.
func captureGuard() (foundryhttp.GuardBinding[Actor], error) {
	transport, guard, err := authfixture.Authentication(ReadCaptures.Registration())
	if err != nil {
		return foundryhttp.GuardBinding[Actor]{}, err
	}
	return foundryhttp.BindGuard(transport, guard)
}
