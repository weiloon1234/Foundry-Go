package softqueries_test

import (
	"foundry.test/consumer/softqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"testing"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(softqueries.Members.Table, softqueries.Groups.Table, softqueries.Memberships.Table); err != nil {
		t.Fatal(err)
	}
	if err := rows.Check(softqueries.MemberLabels.Row); err != nil {
		t.Fatal(err)
	}
}
