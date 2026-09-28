package pagination

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorEndpoint preserves source owner M separately from public DTO T.
type CursorEndpoint[P, F, M, T any] = pageEndpoint[P, F, query.CursorRequest[M], CursorResult[M, T], CursorResponse[T]]

// DefineCursor declares pagination over model or projection M. Supply M explicitly;
// the route, generated filters and item descriptor infer the other concrete types.
// Query execution still validates each token's query scope and authorization must
// run for every request. Tokens are positions, not signed/encrypted credentials.
func DefineCursor[M, P, F, T any](route foundryhttp.Route[P], filters foundryhttp.Query[F], item contract.JSON[T], config CursorConfig) CursorEndpoint[P, F, M, T] {
	if err := config.Validate(); err != nil {
		return CursorEndpoint[P, F, M, T]{err: err}
	}
	validate := func(page query.CursorRequest[M]) error {
		if err := page.Validate(); err != nil {
			return err
		}
		if page.Size > config.MaximumSize {
			return fault.New(fault.Invalid, "page size exceeds endpoint maximum")
		}
		return nil
	}
	result := newPageEndpoint(route, cursorParameters[F, M](filters, config), cursorRules[F, M](config), CursorJSON(item), config.Links, validate, cursorResponse[P, F, M, T])
	result.classify = func(err error) error {
		if errorgraph.Has[*query.CursorInputError](err) {
			return foundryhttp.BadRequest.WithCause(err)
		}
		return err
	}
	return result
}
func cursorResponse[P, F, M, T any](in CursorRequest[P, F, M], page CursorResult[M, T], build func(query.CursorRequest[M]) (string, error)) (CursorResponse[T], error) {
	if !page.defined || page.size != in.Page.Size {
		return CursorResponse[T]{}, fault.New(fault.Invalid, "cursor result does not match its HTTP request")
	}
	var links Links
	if page.next.IsSet() {
		location, err := build(query.CursorRequest[M]{Size: page.size, After: page.next})
		if err != nil {
			return CursorResponse[T]{}, err
		}
		links.Next = value.Of(location)
	}
	if page.previous.IsSet() {
		location, err := build(query.CursorRequest[M]{Size: page.size, Before: page.previous})
		if err != nil {
			return CursorResponse[T]{}, err
		}
		links.Previous = value.Of(location)
	}
	return CursorResponse[T]{Data: pageItems(page.items), Meta: CursorMeta{Size: page.size}, Links: links}, nil
}
