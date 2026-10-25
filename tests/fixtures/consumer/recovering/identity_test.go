package recovering_test

import (
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

var _ model.Identifiable = recovering.Member{}

func TestIdentityRetainsStoredModelAndKey(t *testing.T) {
	id, err := model.ParseID[recovering.Member]("01900000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	got, err := (recovering.Member{ID: id}).FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// This is the persisted name/key codec used by the retired generated method.
	want, err := model.NewReference[recovering.Member]("recovery_members", id, codec.ID[recovering.Member]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("stored model identity changed")
	}
}
