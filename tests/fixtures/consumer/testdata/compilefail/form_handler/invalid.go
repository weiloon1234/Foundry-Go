package invalid

import (
	"context"
	"foundry.test/consumer/requestflow"
)

var _ = requestflow.Submit.Handle(func(context.Context, requestflow.Request) (requestflow.Submission, error) {
	return requestflow.Submission{}, nil
})
