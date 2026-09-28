package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}
type other struct{}

func invalid() {
	left := query.As[alias](models.QueryUsers(), "u")
	right := query.As[other](reports.ProjectUserSummary(models.QueryUsers()).Query(), "s")
	a, b := models.UserFieldsAt(left.Scope()), reports.UserSummaryFieldsAt(right.Scope())
	j := query.LeftJoin(left, right, query.On(a.ID, b.ID))
	_ = reports.UserSummaryFieldsAt(query.NullableRightScope(j, right.Scope()))
}
