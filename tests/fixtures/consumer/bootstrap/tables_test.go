package bootstrap

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMActorDeclarationParity(t *testing.T) {
	if err := orm.Check(Tables()...); err != nil {
		t.Fatal(err)
	}
	got, err := orm.Compile(Members.Query().Where(orm.TextEq(Members.Name, "Ada")))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/bootstrap/tables_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("member SQL changed")
	}
	if MemberIdentity.ReferenceOf(Member{}).ModelName() != "bootstrap_members" {
		t.Fatal("member identity scope changed")
	}
}
