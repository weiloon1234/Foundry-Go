package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type other struct{}

func invalid() { _ = expr.TruncateDate(expr.Value(scalars.Records.Date), expr.TimestampHour) }
