package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

var guard auth.Guard[models.Order]
var _ = authenticating.ViewAccount.Authorize(context.Background(), guard)
