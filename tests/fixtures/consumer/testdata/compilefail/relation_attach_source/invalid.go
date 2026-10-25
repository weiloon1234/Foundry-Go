package invalid

import (
	"context"

	"foundry.test/consumer/linkqueries"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(db *database.DB) {
	_, _ = orm.Attach(context.Background(), db, linkqueries.MemberLinks.Groups, linkqueries.Group{}, linkqueries.Group{})
}
