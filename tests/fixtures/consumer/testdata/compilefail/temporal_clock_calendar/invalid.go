package invalid

import (
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	f := temporalqueries.SampleFields()
	_ = query.Year(f.Clock)
}
