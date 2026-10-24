package invalid

import (
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(user profiles.Profile, scopes auth.AccessScopes[articles.Author]) {
	_, _ = auth.NewScopedProof[profiles.Profile, model.ID[profiles.Profile]](profiles.ProfileIdentity.ReferenceOf(user), auth.Authenticated, scopes)
}
