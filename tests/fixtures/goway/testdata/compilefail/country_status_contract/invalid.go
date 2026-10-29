package country_status_contract

import (
	countries "github.com/weiloon1234/Foundry-Go/countries"
	orm "github.com/weiloon1234/Foundry-Go/database/orm"
)

var status orm.Enum[countries.Country, countries.Status]

func F(raw string) { _ = orm.EnumSet(status, raw) }
