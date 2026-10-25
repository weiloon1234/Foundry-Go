package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(manager *attachments.Manager, collection attachments.Collection[a.Article, int64]) {
	predicate, _ := attachments.Matching(context.Background(), manager, collection)
	_ = a.Authors.Query().Where(predicate)
}
