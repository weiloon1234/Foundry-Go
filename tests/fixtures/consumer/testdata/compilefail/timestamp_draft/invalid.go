package invalid

import (
	"time"

	"foundry.test/consumer/timequeries"
)

var _ = timequeries.Members.UpdatedAt.Set(time.Time{})
