package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var q = models.QueryUsers()

func invalid() {
	var rows []models.Order
	rows, _ = query.SelectRecord(q, q.Scope()).All(context.Background(), nil)
	_ = rows
}
