package nestedbindings_test

import (
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"testing"
)

func TestORMDeclarationsAndBindings(t *testing.T) {
	if err := orm.Check(n.TeamRecords.Table, n.ProjectRecords.Table, n.TaskRecords.Table); err != nil {
		t.Fatal(err)
	}
}
