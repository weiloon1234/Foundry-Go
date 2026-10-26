package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var id model.ID[models.User]
var invalid, _ = models.QueryUsers().Update(context.Background(), nil, id, models.CountryDraft{})
