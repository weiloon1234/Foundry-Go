package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
)

func bad(name auth.GuardName) { _ = auth.DefineAccessScope[models.User](name) }
