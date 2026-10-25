package invalid

import (
	"context"
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(manager *attachments.Manager) {
	predicate, _ := attachments.Matching(context.Background(), manager, profiles.Avatar)
	_ = articles.Authors.Query().Where(predicate)
}
