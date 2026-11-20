package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var filterName = datatable.FilterBy(datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(reporting.MemberModels.Name))))
var invalid = datatable.Column(reporting.MemberRowColumns().Nickname, reporting.NicknameLabel, expr.AsSelected(expr.TextNullableRow(reporting.MemberModels.Nickname)), filterName)
