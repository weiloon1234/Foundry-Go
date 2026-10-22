package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func intervalComparisonKey(v temporal.Interval) string { return querycore.IntervalComparisonKey(v) }
