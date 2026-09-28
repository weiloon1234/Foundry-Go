package invalid

import (
	"context"
	"foundry.test/consumer/limiting"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(l limiting.MemberLimiter, id model.ID[models.User]) {
	_, _ = l.Allow(context.Background(), id)
}
