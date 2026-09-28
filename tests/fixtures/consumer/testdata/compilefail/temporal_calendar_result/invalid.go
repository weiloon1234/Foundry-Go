package invalid

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func invalid() {
	f := temporalqueries.SampleFields()
	_ = query.AddDateInterval(f.Date, temporal.Days(1)).Eq(temporal.Date{})
}
