package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad(scopes auth.AccessScopes[models.User]) { _ = auth.AccessScopes[models.Group](scopes) }
