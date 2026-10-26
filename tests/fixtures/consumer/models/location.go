package models

import "github.com/weiloon1234/Foundry-Go/database/relation"

type LocationCode string

type Location struct {
	Code        LocationCode
	CountryCode CountryCode
	Country     relation.One[Country]
}
