package transactionqueries

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestTransactionDeclarationSQLParity(t *testing.T) {
	type statement interface {
		SQL() string
		Arguments() []any
	}
	compare := func(got statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}

		if !oracleEqual(t, "consumer/transactionqueries/orm_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
	got, err := orm.CompileLocked(Records.Query().OrderBy(Records.ID.Asc()).Limit(1).ForUpdate().SkipLocked())
	compare(got, err)
	projected := rows.Select(rows.From(Records.Query()), Summaries, rows.Map(Summaries.ID, expr.Value(Records.ID)), rows.Map(Summaries.Name, expr.Value(Records.Name)))
	got, err = rows.CompileRead(rows.Read(projected))
	compare(got, err)
}
