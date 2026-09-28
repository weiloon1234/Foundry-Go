package invalid

import (
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = httpkernel.UserPath{User: model.ID[models.Order]{}}
