package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(s redisdata.MemberGroups, id model.ID[mutatorqueries.Member]) []model.ID[models.User] {
	result, _ := s.Members(context.Background(), id)
	return result
}
