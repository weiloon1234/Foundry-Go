package invalid

import (
	"context"

	"foundry.test/consumer/linkqueries"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(db *database.DB) {
	rows, _ := orm.Detach(context.Background(), db, linkqueries.MemberLinks.Groups, linkqueries.Member{}, linkqueries.Group{})
	var wrong []linkqueries.Friendship = rows
	_ = wrong
}
