package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func invalid() {
	f := temporalqueries.Samples
	_ = expr.Where(models.Users.Query(), expr.Year(expr.Value(f.Date)).Eq(2024))
}
