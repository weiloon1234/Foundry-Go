package invalid

import (
	"context"

	"foundry.test/consumer/linkqueries"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func invalid(ctx context.Context, db *database.DB) {
	defaults := []orm.Assignment[linkqueries.Member]{orm.TextSet(linkqueries.Members.Name, "wrong")}
	_, _ = orm.AttachMany(ctx, db, linkqueries.MemberLinks.Groups, linkqueries.Member{}, []linkqueries.Group{}, defaults...)
}
