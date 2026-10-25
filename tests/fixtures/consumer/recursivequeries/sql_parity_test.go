package recursivequeries_test

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/recursivequeries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"
)

func TestRecursiveRecordSQLParity(t *testing.T) {
	if err := orm.Check(recursivequeries.Nodes); err != nil {
		t.Fatal(err)
	}
	tree := rows.RecursiveCTE("descendants", rows.ModelRecord(models.Users.Query().Where(models.Users.Age.Eq(20))), descendants)
	alias := rows.AsRecord[resultAlias](tree, "result")
	selected := rows.SelectRecord(alias, rows.Scope(alias)).OrderBy(expr.At(rows.Scope(alias), models.Users.Email).Asc())
	if err := rows.PreflightRead(t.Context(), selected); err != nil {
		t.Fatal(err)
	}
	got, err := rows.CompileRead(selected)
	if err != nil {
		t.Fatal(err)
	}
	old := query.RecursiveCTE("descendants", models.QueryUsers().Where(models.UserFields().Age.Eq(20)), func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
		p := query.As[parentAlias](self, "parent")
		c := query.As[childAlias](models.QueryUsers(), "child")
		j := query.InnerJoin(c, p, query.On(models.UserFieldsAt(c.Scope()).IntroducerID, models.UserFieldsAt(p.Scope()).ID))
		return query.SelectRecord(j, query.LeftScope(j, c.Scope()))
	})
	a := query.As[resultAlias](old, "result")
	want, err := query.SelectRecord(a, a.Scope()).OrderBy(models.UserFieldsAt(a.Scope()).Email.Asc()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "consumer/recursivequeries/sql_parity_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("recursive SQL parity\n%s\n%s", got.SQL(), want.SQL())
	}
}
