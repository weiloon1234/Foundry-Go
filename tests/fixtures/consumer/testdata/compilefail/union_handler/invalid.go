package invalid

import (
	"context"
	"foundry.test/consumer/unions"
)

var _ = unions.Echo.Handle(func(context.Context, unions.EchoInput) (unions.PaymentRequest, error) {
	return unions.PaymentRequest{}, nil
})
