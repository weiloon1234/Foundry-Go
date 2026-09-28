package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid, _ = models.QueryUsers().CursorPaginate(context.Background(), nil, query.CursorRequest[models.Order]{Size: 2})
