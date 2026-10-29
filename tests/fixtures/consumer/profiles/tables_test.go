package profiles_test

import (
	"bytes"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/cleanupfixture"
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
	baseline := cleanupfixture.Read(t)
	var oldIdentity model.Identity
	if err := json.Unmarshal(baseline.Identity, &oldIdentity); err != nil {
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
	if !bytes.Equal(before, after) || !oracleEqual(t, "consumer/profiles/tables_test/3", oracle.Snapshot(after)) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := profiles.ProfileIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement := baseline.Statements["read"]
	nextStatement, err := orm.Compile(profiles.Profiles.Query().Where(orm.TextEq(profiles.Profiles.Name, "Consumer")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments, nextStatement.Arguments()) || !oracleEqual(t, "consumer/profiles/tables_test/1", oracle.Snapshot([]any{nextStatement.SQL(), nextStatement.Arguments()})) {
		t.Fatal("stored field read changed", oldStatement.SQL, nextStatement.SQL())
	}
}

func TestORMProfileExtensionScopeAndVisibilityParity(t *testing.T) {
	baseline := cleanupfixture.Read(t)
	id, err := model.ParseID[profiles.Profile]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	before := *baseline.Subject
	after, err := profiles.Owners.Subject(profiles.ProfileIdentity.Reference(id))
	if err != nil || !reflect.DeepEqual(before, after) || !oracleEqual(t, "consumer/profiles/tables_test/4", oracle.Snapshot(after)) {
		t.Fatal("profile extension scope changed", err)
	}
	oldStatement := baseline.Statements["trashed"]
	nextStatement, err := orm.Compile(profiles.Profiles.Query().WithTrashed())
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments, nextStatement.Arguments()) || !oracleEqual(t, "consumer/profiles/tables_test/2", oracle.Snapshot([]any{nextStatement.SQL(), nextStatement.Arguments()})) {
		t.Fatal("deleted visibility changed")
	}
}
