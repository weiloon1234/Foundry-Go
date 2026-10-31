package recursivequeries_test

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/recursivequeries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"
)

func TestRecursiveRecordSQLParity(t *testing.T) {
	if err := orm.Check(recursivequeries.Nodes); err != nil {
		t.Fatal(err)
	}
	tree := rows.RecursiveCTE("descendants", rows.ModelRecord(models.Users.Query().Where(orm.NumberEq(models.Users.Age, 20))), descendants)
	alias := rows.AsRecord[resultAlias](tree, "result")
	selected := rows.SelectRecord(alias, rows.Scope(alias)).OrderBy(expr.Asc(expr.At(rows.Scope(alias), orm.TextExpr(models.Users.Email))))
	if err := rows.PreflightRead(t.Context(), selected); err != nil {
		t.Fatal(err)
	}
	got, err := rows.CompileRead(selected)
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/recursivequeries/sql_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
}
