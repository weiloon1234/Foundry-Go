package idempotenthttp

import (
	"reflect"
	"testing"

	"foundry.test/consumer/internal/cleanupfixture"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMDeclarationsPreserveStoredQuery(t *testing.T) {
	if err := orm.Check(Tables()...); err != nil {
		t.Fatal(err)
	}
	prior := cleanupfixture.Read(t).Statements["read"]
	current, err := orm.Compile(Workspaces.Query().Where(Workspaces.Tenant.Eq("tenant")))
	if err != nil {
		t.Fatal(err)
	}
	if prior.SQL != current.SQL() || !reflect.DeepEqual(prior.Arguments, current.Arguments()) {
		t.Fatal("stored query changed", prior.SQL, current.SQL())
	}
}
