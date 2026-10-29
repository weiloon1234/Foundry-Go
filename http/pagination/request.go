package pagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// Parameters retains generated domain filters and the actual ORM request.
type Parameters[F any] = parameters[F, orm.PageRequest]

type parameters[F, W any] struct {
	Filters F
	Page    W
}

// Request is the thin domain-handler input, with concrete path/filter types.
// Page is usable directly by an ORM query's Paginate or SimplePaginate method.
type Request[P, F any] = pageRequest[P, F, orm.PageRequest]

type pageRequest[P, F, W any] struct {
	Path    P
	Filters F
	Page    W
}

var validPageRequest = validation.Custom[orm.PageRequest](validation.Spec{
	ID: "pagination.page_request", Message: "The page request cannot be represented.",
}, func(_ context.Context, request orm.PageRequest) (bool, error) {
	return request.Validate() == nil, nil
})

func pageQuery[F any](filters foundryhttp.Query[F], config Config) foundryhttp.Query[Parameters[F]] {
	return foundryhttp.MergeQueries(
		foundryhttp.EmbedQuery(filters, func(input *Parameters[F]) *F { return &input.Filters }),
		foundryhttp.DefineQuery(
			foundryhttp.DefaultQueryParam(config.NumberParam, foundryhttp.IntegerQuery[int](), 1, func(input *Parameters[F]) *int { return &input.Page.Number }).WithPresentation(labelPresentation(NumberLabelKey)),
			foundryhttp.DefaultQueryParam(config.SizeParam, foundryhttp.IntegerQuery[int](), config.DefaultSize, func(input *Parameters[F]) *int { return &input.Page.Size }).WithPresentation(labelPresentation(SizeLabelKey)),
		),
	)
}
func pageRules[F any](config Config) validation.Rule[Parameters[F]] {
	number := validation.WithLabelKey(validation.WithLabel(validation.DefineField(config.NumberParam, func(request orm.PageRequest) int { return request.Number }), numberLabel), NumberLabelKey)
	size := validation.WithLabelKey(validation.WithLabel(validation.DefineField(config.SizeParam, func(request orm.PageRequest) int { return request.Size }), sizeLabel), SizeLabelKey)
	rule := validation.Bail(
		validation.Rules(number, validation.Min(1), validation.Max(config.maximumPage())),
		validation.Rules(size, validation.Min(1), validation.Max(config.MaximumSize)),
		validPageRequest,
	)
	return validation.Embed(rule, func(input Parameters[F]) orm.PageRequest { return input.Page })
}
func request[P, F, W any](input foundryhttp.Input[P, parameters[F, W], foundryhttp.NoBody]) pageRequest[P, F, W] {
	return pageRequest[P, F, W]{Path: input.Path, Filters: input.Query.Filters, Page: input.Query.Page}
}
