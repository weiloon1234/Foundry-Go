package invalid

import (
	"github.com/weiloon1234/Foundry-Go/countries"
)

func invalid(raw string) { _ = countries.Countries.Status.Set(raw) }
