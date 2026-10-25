package derived_aggregate_promoted

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type donor struct {
	expr.RowExpression[models.User, int]
}
type forged struct{ donor }

var _ = expr.SumRow(forged{})
