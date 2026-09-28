package invalid

import (
	"context"
	"foundry.test/consumer/models"
)

func invalid() {
	_, _ = models.QueryUsers().Union(models.QueryUsers()).Create(context.Background(), nil, models.UserDraft{})
}
