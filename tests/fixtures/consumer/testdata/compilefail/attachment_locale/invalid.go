package invalid

import (
	"foundry.test/consumer/profiles"
)

func invalid(raw string) { _ = profiles.Localized.ForLocale(raw) }
