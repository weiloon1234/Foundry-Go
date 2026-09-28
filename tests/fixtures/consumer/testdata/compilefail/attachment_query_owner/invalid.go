package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/attachments"
)

func invalid(manager *attachments.Manager) {
	predicate, _ := profiles.Avatar.Matching(context.Background(), manager)
	_ = models.QueryUsers().Where(predicate)
}
