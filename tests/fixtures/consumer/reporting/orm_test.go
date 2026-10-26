package reporting

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestOperatorORMIdentityAndLookupParity(t *testing.T) {
	id, err := model.ParseID[Operator]("0193fd8c-2075-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	got, err := OperatorIdentity.Reference(id).Identity()
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "consumer/reporting/identity", oracle.Snapshot(got)) {
		t.Fatal("operator identity changed")
	}
	restored, err := OperatorIdentity.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := restored.Identity()
	if err != nil || !reflect.DeepEqual(roundtrip, got) || !oracleEqual(t, "consumer/reporting/identity_roundtrip", oracle.Snapshot(roundtrip)) {
		t.Fatal("operator identity roundtrip changed", err)
	}
	statement, err := orm.Compile(OperatorModels.Query().Where(OperatorModels.Key().Eq(id)).Limit(1))
	if err != nil {
		t.Fatal(err)
	}
	assertReportStatement(t, statement)
}
