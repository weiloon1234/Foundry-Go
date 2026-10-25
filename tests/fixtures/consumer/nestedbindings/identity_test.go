package nestedbindings_test

import (
	"foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

var _ model.Identifiable = nestedbindings.Team{}

func TestIdentityRetainsStoredModelAndKey(t *testing.T) {
	id, err := model.ParseID[nestedbindings.Team]("01900000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	got, err := (nestedbindings.Team{ID: id}).FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// This is the persisted name/key codec used by the retired generated method.
	want, err := model.NewReference[nestedbindings.Team]("teams", id, codec.ID[nestedbindings.Team]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("stored model identity changed")
	}
}
