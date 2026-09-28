package invalid

import (
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = httpquery.SearchInput{User: model.ID[models.Order]{}}
