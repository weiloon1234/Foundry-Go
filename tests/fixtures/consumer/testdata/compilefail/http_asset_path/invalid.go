package invalid

import (
	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(m h.AssetMount, id model.ID[models.User]) { m.URL(id) }
