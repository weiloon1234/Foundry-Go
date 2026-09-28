package invalid

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
)

var _ = database.WithClock(time.Time{})
