package reporting

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type reportStatement interface {
	SQL() string
	Arguments() []any
}

func assertReportStatement(t *testing.T, got, want reportStatement) {
	t.Helper()
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("report source changed\nnew: %s %v\nold: %s %v", got.SQL(), got.Arguments(), want.SQL(), want.Arguments())
	}
}

func TestReportSourceGolden(t *testing.T) {
	authority := Authority{resolved: true, actor: Operator{TenantID: 7, Active: true, CanView: true, CanExport: true}}
	t.Run("members", func(t *testing.T) {
		source, err := memberSpec().Source(t.Context(), authority)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rows.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		f := MemberFields()
		id := query.NewProjectionField[MemberRow, model.ID[Member]]("id")
		name := query.NewProjectionField[MemberRow, string]("name")
		note := query.NewProjectionField[MemberRow, value.Nullable[string]]("nickname")
		state := query.NewProjectionField[MemberRow, State]("state")
		balance := query.NewProjectionField[MemberRow, decimal.Decimal]("balance")
		old := legacyProjection(QueryReportMembers().Where(f.TenantID.Eq(7)), []query.ProjectionColumn[MemberRow]{id.Column(), name.Column(), note.Column(), state.Column(), balance.Column()}, query.Map(id, query.Value(f.ID)), query.Map(name, query.Trim(f.Name).Value()), query.Map(note, query.Value(f.Nickname)), query.Map(state, query.Value(f.State)), query.Map(balance, query.Value(f.Balance)))
		want, err := old.Compile()
		if err != nil {
			t.Fatal(err)
		}
		assertReportStatement(t, got, want)
	})
	t.Run("orders", func(t *testing.T) {
		source, err := orderSpec().Source(t.Context(), authority)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rows.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		members, orders := query.As[memberSide](QueryReportMembers(), "members"), query.As[orderSide](QueryReportOrders(), "orders")
		m, o := MemberFieldsAt(members.Scope()), OrderFieldsAt(orders.Scope())
		joined := query.InnerJoin(members, orders, query.On(m.ID, o.MemberID))
		member, order := MemberFieldsAt(query.LeftScope(joined, members.Scope())), OrderFieldsAt(query.RightScope(joined, orders.Scope()))
		id := query.NewProjectionField[OrderRow, model.ID[Order]]("id")
		name := query.NewProjectionField[OrderRow, string]("member_name")
		label := query.NewProjectionField[OrderRow, string]("label")
		amount := query.NewProjectionField[OrderRow, decimal.Decimal]("amount")
		old := legacyProjection(joined, []query.ProjectionColumn[OrderRow]{id.Column(), name.Column(), label.Column(), amount.Column()}, query.Map(id, query.Value(order.ID)), query.Map(name, query.Value(member.Name)), query.Map(label, query.Value(order.Label)), query.Map(amount, query.Value(order.Amount))).Where(member.TenantID.Eq(7), order.TenantID.Eq(7))
		want, err := old.Compile()
		if err != nil {
			t.Fatal(err)
		}
		assertReportStatement(t, got, want)
	})
	t.Run("totals", func(t *testing.T) {
		source, err := totalSpec().Source(t.Context(), authority)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rows.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		f := OrderFields()
		base := QueryReportOrders().Where(f.TenantID.Eq(7), OrderRelations().Member.Where(MemberFields().TenantID.Eq(7)).Exists())
		id := query.NewProjectionField[MemberTotal, model.ID[Member]]("member_id")
		count := query.NewProjectionField[MemberTotal, int64]("count")
		total := query.NewProjectionField[MemberTotal, value.Nullable[decimal.Decimal]]("total")
		old := legacyProjection(base, []query.ProjectionColumn[MemberTotal]{id.Column(), count.Column(), total.Column()}, query.Map(id, query.Value(f.MemberID)), query.Map(count, query.Count[Order]().Value()), query.Map(total, query.Sum(f.Amount).Value())).GroupBy(f.MemberID.Group())
		want, err := old.Compile()
		if err != nil {
			t.Fatal(err)
		}
		assertReportStatement(t, got, want)
	})
}

// Compile-only legacy oracle retains the old projection AST without generated SQL helpers.
func legacyProjection[S, R any](source query.ProjectionSource[S], columns []query.ProjectionColumn[R], mappings ...query.ProjectionMapping[S, R]) query.ProjectionQuery[S, R] {
	return query.Project(source, query.DefineProjection(columns, func(database.Row) (R, error) { panic("compile-only legacy projection") }), mappings...)
}
