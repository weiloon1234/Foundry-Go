package httpquery

import (
	"context"
	"net/http"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func normalizeQuery(ctx context.Context, request *http.Request, parameters foundryhttp.Query[SearchInput]) (string, error) {
	// doc-example:begin http-queries-1
	limits := foundryhttp.QueryLimits{Bytes: 4096, Pairs: 64, Issues: 8}

	input, err := foundryhttp.DecodeQuery(parameters, ctx, request.URL.RawQuery, limits)
	if err != nil {
		return "", err
	}

	// input.User retains model.ID[models.User].
	// input.Search distinguishes omission from an explicitly empty string.
	// input.Statuses is an ordinary []models.Status.
	return foundryhttp.EncodeQuery(parameters, ctx, input, limits)
	// doc-example:end http-queries-1
}
