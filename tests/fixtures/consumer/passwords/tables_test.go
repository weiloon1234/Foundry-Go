package passwords_test

import (
	"bytes"
	"reflect"
	"testing"

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
	oldIdentity, err := record.FoundryReference().Identity()
	if err != nil {
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
	if !bytes.Equal(before, after) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := passwords.AccountIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement, err := passwords.QueryPasswordAccounts().Where(passwords.AccountFields().Email.Eq("member@example.test")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	nextStatement, err := orm.Compile(passwords.AccountRecords.Query().Where(passwords.AccountRecords.Email.Eq("member@example.test")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL() != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments(), nextStatement.Arguments()) {
		t.Fatal("stored field read changed", oldStatement.SQL(), nextStatement.SQL())
	}
}
