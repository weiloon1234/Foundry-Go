package pivothooks

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMPivotDeclarationParity(t *testing.T) {
	if err := orm.Check(append(Tables(), ProjectLinks.Members)...); err != nil {
		t.Fatal(err)
	}
	got, err := orm.Compile(Assignments.Query().Where(orm.TextEq(Assignments.Role, "member")))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/pivothooks/tables_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("pivot storage SQL changed")
	}
}
