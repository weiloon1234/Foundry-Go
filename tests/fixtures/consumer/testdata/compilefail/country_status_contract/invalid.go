package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"github.com/weiloon1234/Foundry-Go/countries"
)

func invalid(raw string) { _ = orm.EnumSet(countries.Countries.Status, raw) }
