package httpquery

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

// SearchWindow reuses generated domain filters and adds a concrete page size.
// Framework pagination composes its own page fields through the same APIs.
type SearchWindow struct {
	Filters SearchInput
	Size    int
}

var SearchWindowParameters = foundryhttp.MergeQueries(
	foundryhttp.EmbedQuery(SearchInputDescriptor(), func(input *SearchWindow) *SearchInput { return &input.Filters }),
	foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam("size", foundryhttp.IntegerQuery[int](), 20, func(input *SearchWindow) *int { return &input.Size })),
)
