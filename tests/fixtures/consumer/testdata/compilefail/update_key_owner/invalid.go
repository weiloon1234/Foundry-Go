package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong model.ID[models.Order]
var invalid, _ = models.QueryUsers().Update(context.Background(), nil, wrong, models.UserDraft{})
