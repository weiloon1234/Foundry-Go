package eventqueries_test

import (
	"testing"

	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(eventqueries.Links.Table); err != nil {
		t.Fatal(err)
	}
}
