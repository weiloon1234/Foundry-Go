package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

var invalid = datatable.DefineColumn[reporting.Member, reporting.MemberRow, value.Nullable[string]](reporting.MemberRowValidationFields().Nickname, reporting.NicknameLabel, datatable.FilterBy(datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(orm.TextExpr(reporting.MemberModels.Name))))))
