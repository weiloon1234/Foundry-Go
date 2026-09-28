package invalid

import (
	"context"
	"foundry.test/consumer/caching"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(tags caching.MemberTags, id model.ID[models.User]) {
	_ = tags.Invalidate(context.Background(), id)
}
