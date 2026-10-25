package retrievalqueries_test

import (
	"foundry.test/consumer/retrievalqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"testing"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(retrievalqueries.Members.Table, retrievalqueries.Groups.Table, retrievalqueries.Memberships.Table); err != nil {
		t.Fatal(err)
	}
	if err := rows.Check(retrievalqueries.MemberLabels.Row); err != nil {
		t.Fatal(err)
	}
}
