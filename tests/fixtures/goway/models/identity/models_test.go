package identity_test

import (
	"context"
	"testing"

	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestHandwrittenIdentitySupportsAuthProvider(t *testing.T) {
	if err := orm.Check(a.Members, a.Others); err != nil {
		t.Fatal(err)
	}
	id, err := model.ParseID[a.Member]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	member := a.Member{ID: id}
	provider := auth.DefineProvider("members", a.MemberIdentity.ReferenceOf(member),
		func(context.Context, model.ID[a.Member]) (value.Optional[a.Member], error) {
			return value.Set(member), nil
		}, func(context.Context, a.Member) (bool, error) { return true, nil })
	if err := provider.Validate(); err != nil {
		t.Fatal(err)
	}
	stored, err := member.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	reference, err := provider.Parse(stored)
	if err != nil || reference.Key() != id {
		t.Fatal("provider rejected the handwritten identity", err)
	}
	proof, err := auth.NewProof(reference, auth.Authenticated)
	if err != nil || proof.Identity() != stored {
		t.Fatal("proof changed the stored identity", err)
	}
}
