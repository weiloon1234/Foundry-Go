package invalid

import (
	"foundry.test/consumer/messaging"
)

func wrong(raw string) { _ = messaging.MemberChange{Email: raw} }
