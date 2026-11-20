package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func invalid(label string) {
	_ = datatable.Column(validation.DefineBinding("name", func(r *reporting.Member) *string { return &r.Name }), label, expr.AsSelected(expr.Row(expr.Value(reporting.Members.Name))))
}
