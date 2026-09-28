package invalid

import (
	"context"
	"foundry.test/consumer/models"
)

var invalid, _ = models.QueryCountries().Find(context.Background(), nil, models.StatusActive)
