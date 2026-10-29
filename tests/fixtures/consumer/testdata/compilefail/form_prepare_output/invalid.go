package invalid

import (
	"context"

	"foundry.test/consumer/requestflow"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _ = http.PrepareEndpoint[http.NoPath, requestflow.Search, requestflow.Submission, requestflow.Reply](requestflow.Submit, func(context.Context, requestflow.Request) (requestflow.Submission, requestflow.Search, error) {
	return requestflow.Submission{}, requestflow.Search{}, nil
})
