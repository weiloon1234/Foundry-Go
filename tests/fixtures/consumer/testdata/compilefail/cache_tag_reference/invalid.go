package invalid

import (
	"foundry.test/consumer/caching"
)

func wrong(profiles caching.Profiles) { _, _ = profiles.WithTags("members") }
