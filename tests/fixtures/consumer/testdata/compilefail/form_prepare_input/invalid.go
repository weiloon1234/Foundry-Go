package invalid

import (
	"context"
	"foundry.test/consumer/requestflow"
)

var _ = requestflow.Submit.WithPreparation(func(context.Context, string) (requestflow.Search, requestflow.Submission, error) {
	return requestflow.Search{}, requestflow.Submission{}, nil
})
