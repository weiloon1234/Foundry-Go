package invalid

import (
	"context"
	"foundry.test/consumer/models"
)

var invalid, _ = models.Countries.Find(context.Background(), nil, models.StatusActive)
