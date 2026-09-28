package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	f := temporalqueries.SampleFields()
	_ = models.QueryUsers().Where(query.Year(f.Date).Eq(2024))
}
