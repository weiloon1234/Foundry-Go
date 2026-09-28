package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
)

// A generated declared projection cannot be submitted as a model write draft.
var invalid, _ = models.QueryUsers().Create(context.Background(), nil, reports.UserSummary{Email: "example"})
