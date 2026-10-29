package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestRawFragmentsComposeAndHydrate(t *testing.T) {
	o := declareOrders()
	source := rows.FromModel(o.Query().Where(orm.And(orm.KeyEq(o.ID, 1), orm.Not(orm.WhereRaw[order]("member_id = ?", 2)))))
	selected := expr.Raw[order, int64]("COALESCE(member_id, ?)", 0)
	q := expressionQuery(source, selected, codec.Signed[int64]())
	q = rows.OrderBy(q, selected.Asc())
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(5)}}}
	got, err := rows.Read(q).All(statementmeta.Enable(t.Context()), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 5 {
		t.Fatal(got, err)
	}
	if !strings.Contains(state.sql, "COALESCE(member_id, $1)") || !strings.Contains(state.sql, "member_id = $3") {
		t.Fatal(state.sql)
	}
	nullable := expr.Raw[order, value.Nullable[int64]]("NULL")
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}}}
	result, err := rows.Read(expressionQuery(source, nullable, codec.Nullable(codec.Signed[int64]()))).All(t.Context(), openTest(t, state))
	if err != nil || len(result) != 1 || !result[0].Value.IsNull() {
		t.Fatal(result, err)
	}
	type label string
	custom := expr.RawWithCodec[order](codec.String[label](), "?", "hello")
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"hello"}}}
	labels, err := rows.Read(expressionQuery(source, custom, codec.String[label]())).All(t.Context(), openTest(t, state))
	if err != nil || len(labels) != 1 || labels[0].Value != "hello" {
		t.Fatal(labels, err)
	}
}
func TestRawFragmentsFailBeforeSQL(t *testing.T) {
	o := declareOrders()
	source := rows.FromModel(o.Query())
	for _, selected := range []expr.Selected[order, int64]{expr.Raw[order, int64]("?"), expr.RawWithCodec[order](codec.Codec[int64]{}, "1")} {
		state := &driverState{}
		got, err := rows.Read(expressionQuery(source, selected, codec.Signed[int64]())).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal(got, err, state.sql)
		}
	}
	for _, p := range []orm.Predicate[order]{orm.WhereRaw[order]("?", struct{}{})} {
		state := &driverState{}
		got, err := orm.All(t.Context(), openTest(t, state), o.Query().Where(p))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal(got, err, state.sql)
		}
	}
}
