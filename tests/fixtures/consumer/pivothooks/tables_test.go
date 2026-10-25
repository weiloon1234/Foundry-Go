package pivothooks

import (
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
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatal("pivot storage SQL changed")
	}
}
