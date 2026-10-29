package http

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/idempotency"
)

// Keep concrete wire types across the heterogeneous factory boundary. Auth and
// model-binding adapters retain the underlying transport contract; additional
// signing is permitted, but a declared signed transport cannot lose it.
type contributionContract struct {
	wire        reflect.Type
	method      Method
	path        string
	access      Access
	signed      bool
	idempotency idempotency.Definition
}

func (c contributionContract) accepts(actual contributionContract) bool {
	if c.wire == nil || c.signed && !actual.signed {
		return false
	}
	if c.idempotency.Version != 0 && c.idempotency != actual.idempotency {
		return false
	}
	c.idempotency, actual.idempotency = idempotency.Definition{}, idempotency.Definition{}
	c.signed, actual.signed = false, false
	return c == actual
}

func routeContributionContract[P any](r Route[P]) contributionContract {
	return contributionContract{wire: reflect.TypeFor[Route[P]](), method: RouteMethod(r), path: RoutePattern(r), access: r.spec.Access}
}
func endpointContributionContract[P, Q, B, R any](e Endpoint[P, Q, B, R]) contributionContract {
	result := routeContributionContract(e.route)
	result.wire = reflect.TypeFor[Endpoint[P, Q, B, R]]()
	return result
}
func signedRouteContributionContract[P any](r SignedRoute[P]) contributionContract {
	result := routeContributionContract(r.route)
	result.signed = true
	return result
}
func signedEndpointContributionContract[P, Q, B, R any](e SignedEndpoint[P, Q, B, R]) contributionContract {
	result := endpointContributionContract(e.endpoint)
	result.signed = true
	return result
}
