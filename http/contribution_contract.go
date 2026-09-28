package http

import (
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"reflect"
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

func (r Route[P]) contributionContract() contributionContract {
	return contributionContract{wire: reflect.TypeFor[Route[P]](), method: r.Method(), path: r.Pattern(), access: r.spec.Access}
}
func (e Endpoint[P, Q, B, R]) contributionContract() contributionContract {
	result := e.route.contributionContract()
	result.wire = reflect.TypeFor[Endpoint[P, Q, B, R]]()
	return result
}
func (r SignedRoute[P]) contributionContract() contributionContract {
	result := r.route.contributionContract()
	result.signed = true
	return result
}
func (e SignedEndpoint[P, Q, B, R]) contributionContract() contributionContract {
	result := e.endpoint.contributionContract()
	result.signed = true
	return result
}
