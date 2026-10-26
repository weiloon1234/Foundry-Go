package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}
type other struct{}

func invalid() {
	source := query.As[alias](reports.ProjectUserSummary(models.QueryUsers()).Query(), "s")
	_ = reports.UserSummaryFieldsAt(source.Scope()).Status.Like("active")
}
