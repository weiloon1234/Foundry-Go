package httpquery

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

// SearchWindow reuses generated domain filters and adds a concrete page size.
// Framework pagination composes its own page fields through the same APIs.
// doc-example:begin composed-search-window
type SearchWindow struct {
	Filters SearchInput
	Size    int
}

func composeSearchWindow(filters foundryhttp.Query[SearchInput]) foundryhttp.Query[SearchWindow] {
	return foundryhttp.MergeQueries(
		foundryhttp.EmbedQuery(filters, func(input *SearchWindow) *SearchInput { return &input.Filters }),
		foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam("size", foundryhttp.IntegerQuery[int](), 20, func(input *SearchWindow) *int { return &input.Size })),
	)
}

// doc-example:end composed-search-window

var SearchWindowParameters = composeSearchWindow(SearchInputDescriptor())
