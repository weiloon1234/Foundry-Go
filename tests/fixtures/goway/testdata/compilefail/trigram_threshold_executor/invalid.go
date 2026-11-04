package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var db *database.DB
var _ = expr.SetTrigramThreshold(context.Background(), db, 0.6)
