package invalid

import (
	"context"
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func bad() {
	_ = auth.DefineProvider("profiles", profiles.ProfileIdentity.Reference(model.ID[profiles.Profile]{}), func(context.Context, model.ID[articles.Author]) (value.Optional[profiles.Profile], error) {
		return value.Optional[profiles.Profile]{}, nil
	}, func(context.Context, profiles.Profile) (bool, error) { return true, nil })
}
