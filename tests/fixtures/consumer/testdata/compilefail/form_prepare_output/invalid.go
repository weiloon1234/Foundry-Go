package invalid

import (
	"context"
	"foundry.test/consumer/requestflow"
)

var _ = requestflow.Submit.WithPreparation(func(context.Context, requestflow.Request) (requestflow.Submission, requestflow.Search, error) {
	return requestflow.Submission{}, requestflow.Search{}, nil
})
