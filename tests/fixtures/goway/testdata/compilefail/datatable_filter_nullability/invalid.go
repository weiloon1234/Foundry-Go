package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = datatable.DefineColumn[reporting.Member, reporting.Member, value.Nullable[string]](validation.DefineField("nickname", func(r reporting.Member) value.Nullable[string] { return r.Nickname }), "reports.nickname", datatable.FilterBy(datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(orm.TextExpr(reporting.Members.Name))))))
