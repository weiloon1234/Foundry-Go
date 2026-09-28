package invalid

import (
	"context"
	"foundry.test/consumer/coordination"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"time"
)

func wrong(l coordination.MemberLeases, id model.ID[models.User]) {
	_, _, _ = l.Acquire(context.Background(), id, time.Second, time.Second)
}
