package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type Side struct{}

var _ = rows.As[Side](models.Users.Query().ForUpdate(), "users")
