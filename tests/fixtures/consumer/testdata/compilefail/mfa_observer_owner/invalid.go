package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(f *multifactor.Factors) {
	_, _ = f.WithObserver(mfa.Observer[models.User, model.ID[models.User]]{})
}
