package invalid

import (
	"context"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/settings"
)

func invalid(manager *settings.Manager, input string) {
	_ = profiles.PageSize.Set(context.Background(), manager, input)
}
