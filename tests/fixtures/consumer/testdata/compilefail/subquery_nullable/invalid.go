package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}
type other struct{}

func invalid() {
	_ = models.UserFields().ID.InQuery(query.SelectValue(models.QueryUsers(), models.UserFields().IntroducerID.Value()))
}
