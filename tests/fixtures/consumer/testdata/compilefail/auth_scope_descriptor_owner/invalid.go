package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad(scope auth.AccessScope[models.Group]) { _, _ = auth.NewAccessScopes[models.User](scope) }
