package invalid

import (
	"context"
	"foundry.test/consumer/models"
)

var invalid, _ = models.QueryUsers().Create(context.Background(), nil, models.CountryDraft{})
