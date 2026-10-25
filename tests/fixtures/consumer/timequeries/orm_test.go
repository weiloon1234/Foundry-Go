package timequeries_test

import (
	"foundry.test/consumer/timequeries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"testing"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(timequeries.Members.Table, timequeries.Manuals.Table); err != nil {
		t.Fatal(err)
	}
}
