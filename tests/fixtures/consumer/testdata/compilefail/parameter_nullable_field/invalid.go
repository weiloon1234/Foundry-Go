package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = models.UserFields().Nickname.Param(value.Of("name"))
