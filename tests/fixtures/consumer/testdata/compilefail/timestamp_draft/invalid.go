package invalid

import (
	"time"

	"foundry.test/consumer/timequeries"
)

var _ = timequeries.MemberDraft{}.SetUpdatedAt(time.Time{})
