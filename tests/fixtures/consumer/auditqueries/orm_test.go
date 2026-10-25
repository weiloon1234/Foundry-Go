package auditqueries_test

import (
	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"testing"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(auditqueries.Accounts.Table, auditqueries.Labels.Table); err != nil {
		t.Fatal(err)
	}
}
