// Package countries supplies an ordinary typed country model and an explicitly
// invoked, versioned reference-data seeder. Construction never seeds a database.
package countries

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Code is an uppercase alpha-2 country identifier, including user-assigned codes
// in the bundled reference. ParseCode normalizes explicit transport input.
type Code string

func ParseCode(text string) (Code, error) {
	code := Code(strings.ToUpper(strings.TrimSpace(text)))
	if err := code.Validate(); err != nil {
		return "", err
	}
	return code, nil
}
func (c Code) Validate() error {
	if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
		return invalid()
	}
	return nil
}

//foundry:enum
type Status string

const (
	EnabledStatus  Status = "enabled"
	DisabledStatus Status = "disabled"
)

type Currency struct {
	Code       string                 `json:"code"`
	Name       value.Nullable[string] `json:"name"`
	Symbol     value.Nullable[string] `json:"symbol"`
	MinorUnits value.Nullable[int16]  `json:"minor_units"`
}

// Country uses an explicit ORM declaration with its natural primary key.
// Status, ConversionRate and IsDefault belong to the application; reference
// seeding never overwrites those fields on existing rows.
type Country struct {
	ISO2                Code                   `foundry:"column=iso2"`
	ISO3                string                 `foundry:"column=iso3"`
	ISONumeric          value.Nullable[string] `foundry:"column=iso_numeric"`
	Name                string
	OfficialName        value.Nullable[string]
	Capital             value.Nullable[string]
	Region              value.Nullable[string]
	Subregion           value.Nullable[string]
	Currencies          value.JSON[[]Currency]
	PrimaryCurrencyCode value.Nullable[string]
	CallingCode         value.Nullable[string]
	CallingRoot         value.Nullable[string]
	CallingSuffixes     value.JSON[[]string]
	TLDs                value.JSON[[]string] `foundry:"column=tlds"`
	Timezones           value.JSON[[]string]
	Latitude            value.Nullable[float64]
	Longitude           value.Nullable[float64]
	Independent         value.Nullable[bool]
	UNMember            value.Nullable[bool] `foundry:"column=un_member"`
	FlagEmoji           value.Nullable[string]
	Status              Status
	ConversionRate      value.Nullable[decimal.Decimal]
	IsDefault           bool
	ReferenceVersion    string
	// CreatedAt preserves an explicit creation time; omitted inserts use the application clock.
	// Updates do not automatically change it. Example: Country{CreatedAt: createdAt}.
	CreatedAt temporal.DateTime
	// UpdatedAt is replaced by the application clock on writes, after before hooks and before field mutators.
	// Explicit assignments do not override this management. Example: Countries.UpdatedAt.Desc().
	UpdatedAt temporal.DateTime
}

func invalid() error { return fault.New(fault.Invalid, "invalid country reference data") }

// CountryTable binds the persisted Country columns. Example: Countries.Query().Limit(10).
type CountryTable struct {
	// Table provides Country queries and writes. Example: Countries.Query().Limit(10).
	orm.Table[Country, Code]
	// ISO2 binds the persisted Country.ISO2 column.
	// Example: Countries.ISO2.Eq(iSO2).
	ISO2 orm.Key[Country, Code]
	// ISO3 binds the persisted Country.ISO3 column.
	// Example: Countries.ISO3.Set(iSO3).
	ISO3 orm.Text[Country, string]
	// ISONumeric binds the persisted Country.ISONumeric column.
	// Example: Countries.ISONumeric.SetNull().
	ISONumeric orm.NullableText[Country, string]
	// Name binds the persisted Country.Name column.
	// Example: Countries.Name.Set(name).
	Name orm.Text[Country, string]
	// OfficialName binds the persisted Country.OfficialName column.
	// Example: Countries.OfficialName.SetNull().
	OfficialName orm.NullableText[Country, string]
	// Capital binds the persisted Country.Capital column.
	// Example: Countries.Capital.SetNull().
	Capital orm.NullableText[Country, string]
	// Region binds the persisted Country.Region column.
	// Example: Countries.Region.SetNull().
	Region orm.NullableText[Country, string]
	// Subregion binds the persisted Country.Subregion column.
	// Example: Countries.Subregion.SetNull().
	Subregion orm.NullableText[Country, string]
	// Currencies binds the persisted Country.Currencies column.
	// Example: encoded, err := value.NewJSON(currencies); if err == nil { _ = Countries.Currencies.Set(encoded) }.
	Currencies orm.JSON[Country, []Currency]
	// PrimaryCurrencyCode binds the persisted Country.PrimaryCurrencyCode column.
	// Example: Countries.PrimaryCurrencyCode.SetNull().
	PrimaryCurrencyCode orm.NullableText[Country, string]
	// CallingCode binds the persisted Country.CallingCode column.
	// Example: Countries.CallingCode.SetNull().
	CallingCode orm.NullableText[Country, string]
	// CallingRoot binds the persisted Country.CallingRoot column.
	// Example: Countries.CallingRoot.SetNull().
	CallingRoot orm.NullableText[Country, string]
	// CallingSuffixes binds the persisted Country.CallingSuffixes column.
	// Example: encoded, err := value.NewJSON(callingSuffixes); if err == nil { _ = Countries.CallingSuffixes.Set(encoded) }.
	CallingSuffixes orm.JSON[Country, []string]
	// TLDs binds the persisted Country.TLDs column.
	// Example: encoded, err := value.NewJSON([]string{".my"}); if err == nil { _ = Countries.TLDs.Set(encoded) }.
	TLDs orm.JSON[Country, []string]
	// Timezones binds the persisted Country.Timezones column.
	// Example: encoded, err := value.NewJSON(timezones); if err == nil { _ = Countries.Timezones.Set(encoded) }.
	Timezones orm.JSON[Country, []string]
	// Latitude binds the persisted Country.Latitude column.
	// Example: Countries.Latitude.SetNull().
	Latitude orm.NullableNumber[Country, float64]
	// Longitude binds the persisted Country.Longitude column.
	// Example: Countries.Longitude.SetNull().
	Longitude orm.NullableNumber[Country, float64]
	// Independent binds the persisted Country.Independent column.
	// Example: Countries.Independent.SetNull().
	Independent orm.NullableColumn[Country, bool]
	// UNMember binds the persisted Country.UNMember column.
	// Example: Countries.UNMember.SetNull().
	UNMember orm.NullableColumn[Country, bool]
	// FlagEmoji binds the persisted Country.FlagEmoji column.
	// Example: Countries.FlagEmoji.SetNull().
	FlagEmoji orm.NullableText[Country, string]
	// Status binds the persisted Country.Status column.
	// Example: Countries.Status.Set(status).
	Status orm.Enum[Country, Status]
	// ConversionRate binds the persisted Country.ConversionRate column.
	// Example: Countries.ConversionRate.SetNull().
	ConversionRate orm.NullableColumn[Country, decimal.Decimal]
	// IsDefault binds the persisted Country.IsDefault column.
	// Example: Countries.IsDefault.Set(isDefault).
	IsDefault orm.Column[Country, bool]
	// ReferenceVersion binds the persisted Country.ReferenceVersion column.
	// Example: Countries.ReferenceVersion.Set(referenceVersion).
	ReferenceVersion orm.Text[Country, string]
	// CreatedAt preserves an explicitly assigned creation time; when omitted on insert, the ORM uses the application clock.
	// The insert default is applied after before hooks and before field mutators; updates do not automatically change it.
	// Example: Countries.CreatedAt.Set(createdAt).
	CreatedAt orm.Time[Country, temporal.DateTime]
	// UpdatedAt is managed: writes replace assigned or omitted values with the application clock after before hooks and before field mutators.
	// Example: Countries.UpdatedAt.Desc().
	UpdatedAt orm.Time[Country, temporal.DateTime]
}

// Countries owns the immutable Country declaration. Example: Countries.Query().First(ctx, tx).
var Countries = orm.Define("foundry_countries", func(b orm.Builder[Country], m *Country) CountryTable {
	table := orm.TableOf(b, &m.ISO2)
	return CountryTable{Table: table,
		ISO2:                table.Key(),
		ISO3:                orm.TextOf(b, &m.ISO3),
		ISONumeric:          orm.NullableTextOf(b, &m.ISONumeric),
		Name:                orm.TextOf(b, &m.Name),
		OfficialName:        orm.NullableTextOf(b, &m.OfficialName),
		Capital:             orm.NullableTextOf(b, &m.Capital),
		Region:              orm.NullableTextOf(b, &m.Region),
		Subregion:           orm.NullableTextOf(b, &m.Subregion),
		Currencies:          orm.JSONOf(b, &m.Currencies),
		PrimaryCurrencyCode: orm.NullableTextOf(b, &m.PrimaryCurrencyCode),
		CallingCode:         orm.NullableTextOf(b, &m.CallingCode),
		CallingRoot:         orm.NullableTextOf(b, &m.CallingRoot),
		CallingSuffixes:     orm.JSONOf(b, &m.CallingSuffixes),
		TLDs:                orm.JSONOf(b, &m.TLDs),
		Timezones:           orm.JSONOf(b, &m.Timezones),
		Latitude:            orm.NullableNumberOf(b, &m.Latitude),
		Longitude:           orm.NullableNumberOf(b, &m.Longitude),
		Independent:         orm.NullableColumnOf(b, &m.Independent),
		UNMember:            orm.NullableColumnOf(b, &m.UNMember),
		FlagEmoji:           orm.NullableTextOf(b, &m.FlagEmoji),
		Status:              orm.EnumOf(b, &m.Status),
		ConversionRate:      orm.NullableColumnOf(b, &m.ConversionRate),
		IsDefault:           orm.ColumnOf(b, &m.IsDefault),
		ReferenceVersion:    orm.TextOf(b, &m.ReferenceVersion),
		CreatedAt:           orm.TimeOf(b, &m.CreatedAt),
		UpdatedAt:           orm.TimeOf(b, &m.UpdatedAt),
	}
})
