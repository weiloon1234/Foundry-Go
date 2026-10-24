package invalid

import (
	"foundry.test/consumer/inputqueries"
)

var _ = inputqueries.Members.Email.Set(inputqueries.StoredEmail("stored"))
