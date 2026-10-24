package multifactor_test

import (
	"bytes"
	"reflect"
	"testing"

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
	oldIdentity, err := record.FoundryReference().Identity()
	if err != nil {
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
	if !bytes.Equal(before, after) {
		t.Fatal("persisted reference changed")
	}
	parsed, err := multifactor.AccountIdentity.Parse(oldIdentity)
	if err != nil || parsed.Key() != id {
		t.Fatal("existing reference rejected", err)
	}
	oldStatement, err := multifactor.QueryMfaAccounts().Where(multifactor.AccountFields().Email.Eq("member@example.test")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	nextStatement, err := orm.Compile(multifactor.AccountRecords.Query().Where(multifactor.AccountRecords.Email.Eq("member@example.test")))
	if err != nil {
		t.Fatal(err)
	}
	if oldStatement.SQL() != nextStatement.SQL() || !reflect.DeepEqual(oldStatement.Arguments(), nextStatement.Arguments()) {
		t.Fatal("stored field read changed", oldStatement.SQL(), nextStatement.SQL())
	}
}
