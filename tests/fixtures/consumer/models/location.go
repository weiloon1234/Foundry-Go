package models

import "github.com/weiloon1234/Foundry-Go/database/relation"

type LocationCode string

//foundry:model table=locations var=Locations key=Code identity=false
type Location struct {
	Code        LocationCode `foundry:"member=Code,key-role=primary"`
	CountryCode CountryCode
	Country     relation.One[Country]
}
