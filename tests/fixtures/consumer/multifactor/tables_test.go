package multifactor_test

import (
	"bytes"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/cleanupfixture"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestORMFoundationParity(t *testing.T) {
	if err := orm.Check(multifactor.Tables()...); err != nil {
		t.Fatal(err)
	}
	id, err := model.ParseID[multifactor.Account]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	record := multifactor.Account{ID: id}
	baseline := cleanupfixture.Read(t)
	var oldIdentity model.Identity
	if err := json.Unmarshal(baseline.Identity, &oldIdentity); err != nil {
		t.Fatal(err)
	}
	nextIdentity, err := multifactor.AccountIdentity.ReferenceOf(record).Identity()
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
	if !bytes.Equal(before, after) || !oracleEqual(t, "consumer/multifactor/tables_test/2", oracle.Snapshot(after)) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := multifactor.AccountIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement := baseline.Statements["read"]
	nextStatement, err := orm.Compile(multifactor.AccountRecords.Query().Where(multifactor.AccountRecords.Email.Eq("member@example.test")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments, nextStatement.Arguments()) || !oracleEqual(t, "consumer/multifactor/tables_test/1", oracle.Snapshot([]any{nextStatement.SQL(), nextStatement.Arguments()})) {
		t.Fatal("stored field read changed", oldStatement.SQL, nextStatement.SQL())
	}
}
