package invalid

import (
	"context"
	"foundry.test/consumer/genericdto"
)

var _ = genericdto.Echo.Handle(func(context.Context, genericdto.EchoInput) (genericdto.Envelope[genericdto.ProjectDTO], error) {
	return genericdto.Envelope[genericdto.ProjectDTO]{}, nil
})
