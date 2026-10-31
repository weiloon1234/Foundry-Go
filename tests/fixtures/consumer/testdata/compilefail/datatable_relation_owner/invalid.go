package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.Related[reporting.Member, reporting.Order, string](datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(orm.TextExpr(reporting.OrderModels.Label)))), func(p expr.Predicate[reporting.Member]) expr.Predicate[reporting.Member] { return p })
