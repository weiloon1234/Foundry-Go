package profiles_test

import (
	"bytes"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"reflect"
	"testing"

	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestORMFoundationParity(t *testing.T) {
	if err := orm.Check(profiles.Tables()...); err != nil {
		t.Fatal(err)
	}
	id, err := model.ParseID[profiles.Profile]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	record := profiles.Profile{ID: id}
	oldIdentity, err := record.FoundryReference().Identity()
	if err != nil {
		t.Fatal(err)
	}
	nextIdentity, err := profiles.ProfileIdentity.ReferenceOf(record).Identity()
	if err != nil {
		t.Fatal(err)
	}
	before, err := oldIdentity.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	after, err := nextIdentity.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := profiles.ProfileIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement, err := profiles.QueryProfiles().Where(profiles.ProfileFields().Name.Eq("Consumer")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	nextStatement, err := orm.Compile(profiles.Profiles.Query().Where(profiles.Profiles.Name.Eq("Consumer")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL() != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments(), nextStatement.Arguments()) {
		t.Fatal("stored field read changed", oldStatement.SQL(), nextStatement.SQL())
	}
}

func TestORMProfileExtensionScopeAndVisibilityParity(t *testing.T) {
	prior := extensions.DefineOwnerWith("profiles", query.IdentityOf(profiles.QueryProfiles().Query, profiles.ProfileFields().ID), extensions.OwnerOptions{StorageModel: "profiles"})
	id, err := model.ParseID[profiles.Profile]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	before, err := prior.Subject(prior.Reference(id))
	if err != nil {
		t.Fatal(err)
	}
	after, err := profiles.Owners.Subject(profiles.ProfileIdentity.Reference(id))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("profile extension scope changed", err)
	}
	oldStatement, err := profiles.QueryProfiles().WithTrashed().Compile()
	if err != nil {
		t.Fatal(err)
	}
	nextStatement, err := orm.Compile(profiles.Profiles.Query().WithTrashed())
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL() != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments(), nextStatement.Arguments()) {
		t.Fatal("deleted visibility changed")
	}
}
