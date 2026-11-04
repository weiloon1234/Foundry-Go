package trigramqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Normalized string

type Entity struct {
	ID      int64
	Country string
}
type EntityName struct {
	ID         int64
	EntityID   int64
	Normalized Normalized
}
type Address struct {
	ID        int64
	EntityID  int64
	Latitude  decimal.Decimal
	Longitude decimal.Decimal
}
type Review struct {
	ID           int64
	Body         string
	OptionalBody value.Nullable[string]
}
type EntityTable struct {
	orm.Table[Entity, int64]
	Country orm.Text[Entity, string]
}
type NameTable struct {
	orm.Table[EntityName, int64]
	EntityID   orm.Number[EntityName, int64]
	Normalized orm.Text[EntityName, Normalized]
}
type AddressTable struct {
	orm.Table[Address, int64]
	EntityID  orm.Number[Address, int64]
	Latitude  orm.Number[Address, decimal.Decimal]
	Longitude orm.Number[Address, decimal.Decimal]
}
type ReviewTable struct {
	orm.Table[Review, int64]
	Body         orm.Text[Review, string]
	OptionalBody orm.NullableText[Review, string]
}
type Tables struct {
	Entities  EntityTable
	Names     NameTable
	Addresses AddressTable
	Reviews   ReviewTable
}

func Declare(schema string) Tables {
	return Tables{
		Entities: orm.Define(schema+".entities", func(b orm.Builder[Entity], m *Entity) EntityTable {
			return EntityTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Country)}
		}),
		Names: orm.Define(schema+".entity_names", func(b orm.Builder[EntityName], m *EntityName) NameTable {
			return NameTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.EntityID), orm.TextOf(b, &m.Normalized)}
		}),
		Addresses: orm.Define(schema+".addresses", func(b orm.Builder[Address], m *Address) AddressTable {
			return AddressTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.EntityID), orm.NumberOf(b, &m.Latitude), orm.NumberOf(b, &m.Longitude)}
		}),
		Reviews: orm.Define(schema+".reviews", func(b orm.Builder[Review], m *Review) ReviewTable {
			return ReviewTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Body), orm.NullableTextOf(b, &m.OptionalBody)}
		}),
	}
}

type Duplicate struct {
	ID         int64
	Normalized Normalized
	Similarity float64
	Latitude   decimal.Decimal
	Longitude  decimal.Decimal
}
type DuplicateFields struct {
	rows.Row[Duplicate]
	ID         rows.Field[Duplicate, int64]
	Normalized rows.Field[Duplicate, Normalized]
	Similarity rows.Field[Duplicate, float64]
	Latitude   rows.Field[Duplicate, decimal.Decimal]
	Longitude  rows.Field[Duplicate, decimal.Decimal]
}

var Duplicates = rows.Define(func(b rows.Builder[Duplicate], r *Duplicate) DuplicateFields {
	return DuplicateFields{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Normalized), rows.NumberOf(b, &r.Similarity), rows.NumberOf(b, &r.Latitude), rows.NumberOf(b, &r.Longitude)}
})
