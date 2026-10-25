package pivothooks

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMPivotDeclarationParity(t *testing.T) {
	if err := orm.Check(append(Tables(), ProjectLinks.Members)...); err != nil {
		t.Fatal(err)
	}
	got, err := orm.Compile(Assignments.Query().Where(Assignments.Role.Eq("member")))
	if err != nil {
		t.Fatal(err)
	}
	want, err := QueryHookAssignments().Where(AssignmentFields().Role.Eq("member")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/pivothooks/tables_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("pivot storage SQL changed")
	}
}
