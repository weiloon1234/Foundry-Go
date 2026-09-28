package invalid

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var wrong validation.Rule[model.ID[models.Order]]
var _ = httpdto.UserResponseValidationFields().ID.Rules(wrong)
