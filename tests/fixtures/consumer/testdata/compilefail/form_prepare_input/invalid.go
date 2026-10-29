package invalid

import (
	"context"

	"foundry.test/consumer/requestflow"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _ = http.PrepareEndpoint[http.NoPath, requestflow.Search, requestflow.Submission, requestflow.Reply](requestflow.Submit, func(context.Context, string) (requestflow.Search, requestflow.Submission, error) {
	return requestflow.Search{}, requestflow.Submission{}, nil
})
