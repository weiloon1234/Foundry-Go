package invalid

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	f := temporalqueries.SampleFields()
	_ = temporalqueries.QueryTemporalSamples().Where(query.OrderNullableValue(query.YearNullableValue(f.Date.Max().Value())).Gt(2024))
}
