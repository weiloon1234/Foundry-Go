package invalid

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	f := temporalqueries.SampleFields()
	_ = temporalqueries.QueryTemporalSamples().Where(query.OrderNullableValue(query.YearNullableValue(query.Max(f.Date).Value())).Gt(2024))
}
