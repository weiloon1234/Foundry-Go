package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type OtherActor struct{ ID int64 }

var _ = httppagination.AuthenticatedNumbered(foundryhttp.GuardBinding[httppagination.Actor]{}).WithAuthorization(func(context.Context, OtherActor, httppagination.ListRequest) error { return nil })
