package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
)

func bad(scopes auth.AccessScopes[models.Group]) { _ = token.IssueOptions[models.User]{Scopes: scopes} }
