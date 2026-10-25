package bootstrap

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMActorDeclarationParity(t *testing.T) {
	if err := orm.Check(Tables()...); err != nil {
		t.Fatal(err)
	}
	got, err := orm.Compile(Members.Query().Where(Members.Name.Eq("Ada")))
	if err != nil {
		t.Fatal(err)
	}
	want, err := QueryBootstrapMembers().Where(MemberFields().Name.Eq("Ada")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatal("member SQL changed")
	}
	if MemberIdentity.ReferenceOf(Member{}).ModelName() != (Member{}).FoundryReference().ModelName() {
		t.Fatal("member identity scope changed")
	}
}
