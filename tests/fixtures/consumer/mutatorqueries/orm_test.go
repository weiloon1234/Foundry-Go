package mutatorqueries_test

import (
	"testing"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestORMMutatorDeclarationPreservesStoredIdentityAndChanges(t *testing.T) {
	if err := orm.Check(mutatorqueries.Members); err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	before := mutatorqueries.Member{ID: id, Email: "ada@example.test", State: mutatorqueries.Active}
	after := before
	after.Nickname = value.Of("!")
	changes, err := orm.Compare(mutatorqueries.Members.Table, value.Set(before), value.Set(after), mutatorqueries.Members.Email.Set(" ADA@EXAMPLE.TEST "), mutatorqueries.Members.Nickname.Set(""))
	if err != nil {
		t.Fatal(err)
	}
	email, err := orm.ChangeOf(changes, mutatorqueries.Members.Email)
	if err != nil {
		t.Fatal(err)
	}
	nickname, err := orm.NullableChangeOf(changes, mutatorqueries.Members.Nickname)
	if err != nil {
		t.Fatal(err)
	}
	if !email.Assigned() || email.Changed() || !nickname.Changed() {
		t.Fatal("stored comparison used fresh mutation inputs")
	}
	if got, ok := nickname.After().Get(); !ok || got != value.Of("!") {
		t.Fatal("comparison ran mutator again")
	}
	reference := mutatorqueries.MemberIdentity.ReferenceOf(before)
	if reference.Key() != id {
		t.Fatal("accessor replaced stored key")
	}
	identity, err := reference.Identity()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mutatorqueries.MemberIdentity.Parse(identity)
	if err != nil || parsed.Key() != id {
		t.Fatal("identity codec changed", err)
	}
	if _, err := orm.Compile(mutatorqueries.Members.Query().Where(mutatorqueries.Members.Email.Eq(before.Email), mutatorqueries.Members.State.Eq(mutatorqueries.Active))); err != nil {
		t.Fatal(err)
	}
	response, err := responseForMember(before)
	if err != nil || response.Email != "ADA@EXAMPLE.TEST" || before.Email != "ada@example.test" {
		t.Fatal("explicit accessor contract changed", err)
	}
}
