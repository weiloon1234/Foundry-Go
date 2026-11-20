package reporting

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
)

const (
	OrderLineMemberLabel i18n.MessageKey = "reports.line.member"
	OrderLineLengthLabel i18n.MessageKey = "reports.line.name_length"
	OrderLineScopeLabel  i18n.MessageKey = "reports.line.scope"
	DistinctNameLabel    i18n.MessageKey = "reports.names.name"
)

//foundry:dto role=row
type OrderLineRow struct {
	ID         model.ID[Order]  `json:"id"`
	MemberID   model.ID[Member] `json:"memberId"`
	MemberName string           `json:"memberName"`
	NameLength int64            `json:"nameLength"`
	Scope      string           `json:"scope"`
}

//foundry:dto role=row
type MemberNameRow struct {
	Name string `json:"name"`
}

// doc-example:begin dynamic-table
func orderLineSpec() datatable.Spec[OrderScope, OrderLineRow, Authority] {
	joined, member, order := orderJoin()
	w := OrderLineRowColumns()
	memberName := expr.TextRowAt(member, MemberModels.Name)
	scope := datatable.NewDynamicSlot[OrderScope, string]()
	return datatable.Spec[OrderScope, OrderLineRow, Authority]{
		ID: "reports.order_lines", Row: OrderLineRowJSON(),
		Columns: []datatable.ColumnRegistration[OrderScope, OrderLineRow]{
			datatable.HiddenSelected(w.ID, expr.AsSelected(expr.Row(expr.At(order, OrderModels.ID)))),
			datatable.HiddenSelected(w.MemberID, expr.AsSelected(expr.Row(expr.At(order, OrderModels.MemberID)))),
			datatable.Column(w.MemberName, OrderLineMemberLabel, expr.AsSelected(memberName),
				datatable.SortBy(expr.AsSelected(memberName))),
			datatable.Column(w.NameLength, OrderLineLengthLabel, expr.LengthValue(expr.AsSelected(memberName)),
				datatable.ScanWith[OrderScope](orm.Codec(codec.Signed[int64]()))),
			datatable.DynamicColumn(w.Scope, OrderLineScopeLabel, scope,
				datatable.SortBy(expr.AsSelected(memberName))),
		},
		Dynamic: []datatable.DynamicBinding[OrderScope, Authority]{
			datatable.BindDynamic(scope, func(ctx context.Context, authority Authority) expr.Selected[OrderScope, string] {
				operator, _ := authority.subject(ctx) // Authorize has already run
				return expr.AsSelected(expr.Constant[OrderScope](codec.String[string](), fmt.Sprintf("tenant-%d", operator.TenantID)))
			}),
		},
		Stable:    []expr.ProjectionOrder[OrderScope]{expr.Asc(expr.At(order, OrderModels.ID))},
		Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.SelectionSource[OrderScope], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return nil, err
			}
			return rows.WhereSource(joined, expr.Eq(expr.At(order, OrderModels.TenantID), actor.TenantID)), nil
		},
	}
}

// doc-example:end dynamic-table

// doc-example:begin distinct-table
func memberNameSpec() datatable.Spec[Member, MemberNameRow, Authority] {
	w := MemberNameRowColumns()
	return datatable.Spec[Member, MemberNameRow, Authority]{
		ID: "reports.member_names", Row: MemberNameRowJSON(),
		Columns: []datatable.ColumnRegistration[Member, MemberNameRow]{
			datatable.Text(w.Name, DistinctNameLabel, MemberModels.Name),
		},
		Stable:    []expr.ProjectionOrder[Member]{expr.Asc(expr.Value(MemberModels.Name))},
		Authorize: authorize,
		Source: func(ctx context.Context, authority Authority) (rows.SelectionSource[Member], error) {
			actor, err := authority.subject(ctx)
			if err != nil {
				return nil, err
			}
			return rows.DistinctSource(rows.From(MemberModels.Query().Where(orm.Eq(MemberModels.TenantID, actor.TenantID)))), nil
		},
	}
}

// doc-example:end distinct-table
