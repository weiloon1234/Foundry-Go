package passwords_test

import (
	"bytes"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/cleanupfixture"
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestORMFoundationParity(t *testing.T) {
	if err := orm.Check(passwords.Tables()...); err != nil {
		t.Fatal(err)
	}
	id, err := model.ParseID[passwords.Account]("018f0000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	record := passwords.Account{ID: id}
	baseline := cleanupfixture.Read(t)
	var oldIdentity model.Identity
	if err := json.Unmarshal(baseline.Identity, &oldIdentity); err != nil {
		t.Fatal(err)
	}
	nextIdentity, err := passwords.AccountIdentity.ReferenceOf(record).Identity()
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
	if !bytes.Equal(before, after) || !oracleEqual(t, "consumer/passwords/tables_test/2", oracle.Snapshot(before), oracle.Snapshot(after)) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := passwords.AccountIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement := baseline.Statements["read"]
	nextStatement, err := orm.Compile(passwords.AccountRecords.Query().Where(passwords.AccountRecords.Email.Eq("member@example.test")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments, nextStatement.Arguments()) || !oracleEqual(t, "consumer/passwords/tables_test/1", oracle.Snapshot([]any{oldStatement.SQL, oldStatement.Arguments}), oracle.Snapshot([]any{nextStatement.SQL(), nextStatement.Arguments()})) {
		t.Fatal("stored field read changed", oldStatement.SQL, nextStatement.SQL())
	}
}
