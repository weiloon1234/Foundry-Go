package hookqueries_test

import (
	"foundry.test/consumer/hookqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"testing"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(hookqueries.Members.Table, hookqueries.Logs.Table); err != nil {
		t.Fatal(err)
	}
}
