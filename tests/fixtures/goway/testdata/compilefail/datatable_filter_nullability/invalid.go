package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

var option = datatable.FilterBy(datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(reporting.Members.Name))))
var invalid = datatable.Column(validation.DefineBinding("nickname", func(r *reporting.Member) *value.Nullable[string] { return &r.Nickname }), "reports.nickname", expr.AsSelected(expr.Row(expr.Value(reporting.Members.Nickname))), option)
