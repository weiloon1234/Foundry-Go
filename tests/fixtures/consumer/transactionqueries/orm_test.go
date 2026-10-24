package transactionqueries

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestTransactionDeclarationSQLParity(t *testing.T) {
	type statement interface {
		SQL() string
		Arguments() []any
	}
	compare := func(got statement, err error, legacy func() (query.Statement, error)) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		want, err := legacy()
		if err != nil {
			t.Fatal(err)
		}
		if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
			t.Fatalf("record SQL changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
		}
	}
	got, err := orm.CompileLocked(Records.Query().OrderBy(Records.ID.Asc()).Limit(1).ForUpdate().SkipLocked())
	compare(got, err, QueryRecords().OrderBy(RecordFields().ID.Asc()).Limit(1).ForUpdate().SkipLocked().Compile)
	projected := rows.Select(rows.From(Records.Query()), Summaries, rows.Map(Summaries.ID, expr.Value(Records.ID)), rows.Map(Summaries.Name, expr.Value(Records.Name)))
	got, err = rows.Compile(projected)
	compare(got, err, ProjectSummary(QueryRecords()).SelectID(query.Value(RecordFields().ID)).SelectName(query.Value(RecordFields().Name)).Query().Compile)
}
