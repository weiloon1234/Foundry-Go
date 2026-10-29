// Frozen v0.1 replay declarations. Generic E stands in for the DTO parameters.
package modelbinding

type Endpoint[E any] struct{}
type AuthenticatedEndpoint[E any] struct{}
type IdempotentEndpoint[E any] struct{}
type AuthenticatedIdempotentEndpoint[E any] struct{}

func Bind[E any](e E, resolver int) Endpoint[E] { return Endpoint[E]{} }
func BindAuthenticated[E any](e E, resolver int) AuthenticatedEndpoint[E] {
	return AuthenticatedEndpoint[E]{}
}
func Idempotent[E any](e Endpoint[E], store, definition int) IdempotentEndpoint[E] {
	return IdempotentEndpoint[E]{}
}
func IdempotentAuthenticated[E any](e AuthenticatedEndpoint[E], store, definition int) AuthenticatedIdempotentEndpoint[E] {
	return AuthenticatedIdempotentEndpoint[E]{}
}
func (e Endpoint[E]) WithAuthorization(policy func() error) Endpoint[E] { return e }
func (e AuthenticatedEndpoint[E]) WithAuthorization(policy func() error) AuthenticatedEndpoint[E] {
	return e
}
func (e IdempotentEndpoint[E]) WithHeaders(headers func() int) IdempotentEndpoint[E] { return e }
func (e AuthenticatedIdempotentEndpoint[E]) WithHeaders(headers func() int) AuthenticatedIdempotentEndpoint[E] {
	return e
}
func (e Endpoint[E]) Handle(handler int) int                               { return 0 }
func (e AuthenticatedEndpoint[E]) Handle(handler int) int                  { return 0 }
func (e IdempotentEndpoint[E]) Handle(scope, handler int) int              { return 0 }
func (e AuthenticatedIdempotentEndpoint[E]) Handle(scope, handler int) int { return 0 }
func (e IdempotentEndpoint[E]) Validate() error                            { return nil }
func (e AuthenticatedIdempotentEndpoint[E]) Validate() error               { return nil }
func (e IdempotentEndpoint[E]) Description() (int, error)                  { return 0, nil }
func (e AuthenticatedIdempotentEndpoint[E]) Description() (int, error)     { return 0, nil }
