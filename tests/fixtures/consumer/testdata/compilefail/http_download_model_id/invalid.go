package invalid

import (
	"context"
	"foundry.test/consumer/httpdownloads"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(documents httpdownloads.Documents, id model.ID[models.Order]) {
	documents.File(context.Background(), id)
}
