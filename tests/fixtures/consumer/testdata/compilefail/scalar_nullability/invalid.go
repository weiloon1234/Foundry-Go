package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}
type other struct{}

func invalid() {
	_ = reports.ProjectUserSummary(models.QueryUsers()).SelectID(query.ScalarQuery(models.QueryUsers(), query.SelectValue(models.QueryUsers(), models.UserFields().ID.Value())))
}
