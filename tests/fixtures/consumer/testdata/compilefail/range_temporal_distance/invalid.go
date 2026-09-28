package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"time"
)

var _ = query.NullableTemporalRange(query.WindowFor(models.QueryUsers()), models.UserFields().Birthday.Value()).Preceding(time.Hour)
