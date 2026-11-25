// Package countries supplies an ordinary typed country model and an explicitly
// invoked, versioned reference-data seeder. Construction never seeds a database.
package countries

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Code is an uppercase alpha-2 country identifier, including user-assigned codes
// in the bundled reference. ParseCode normalizes explicit transport input.
type Code string

// ParseCode trims and uppercases text, then validates the two-letter code.
func ParseCode(text string) (Code, error) {
	code := Code(strings.ToUpper(strings.TrimSpace(text)))
	if err := code.Validate(); err != nil {
		return "", err
	}
	return code, nil
}

// Validate checks that the code consists of two uppercase ASCII letters.
func (c Code) Validate() error {
	if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
		return invalid()
	}
	return nil
}

// Status is a country's status value.
//
//foundry:enum
type Status string

const (
	// EnabledStatus marks a country that Enabled returns.
	EnabledStatus Status = "enabled"
	// DisabledStatus marks a country that Enabled leaves out. Seeding inserts new reference countries with it.
	DisabledStatus Status = "disabled"
)

// Currency describes a country's currency and optional reference metadata.
type Currency struct {
	Code       string                 `json:"code"`
	Name       value.Nullable[string] `json:"name"`
	Symbol     value.Nullable[string] `json:"symbol"`
	MinorUnits value.Nullable[int16]  `json:"minor_units"`
}

// Country uses an explicit ORM declaration with its natural primary key.
// Status, ConversionRate and IsDefault belong to the application; reference
// seeding never overwrites those fields on existing rows.
//
//foundry:model table=foundry_countries var=Countries key=ISO2 identity=false
type Country struct {
	// Table provides Country queries and writes. Example: Countries.Query().Limit(10).
	//
	// ISO2 binds the persisted Country.ISO2 column.
	// Example: orm.Eq(Countries.ISO2, iSO2).
	ISO2 Code `foundry:"member=ISO2,key-role=lookup"`
	// ISO3 binds the persisted Country.ISO3 column.
	// Example: orm.Set(Countries.ISO3, iSO3).
	ISO3 string
	// ISONumeric binds the persisted Country.ISONumeric column.
	// Example: orm.SetNull(Countries.ISONumeric).
	ISONumeric value.Nullable[string]
	// Name binds the persisted Country.Name column.
	// Example: orm.Set(Countries.Name, name).
	Name string
	// OfficialName binds the persisted Country.OfficialName column.
	// Example: orm.SetNull(Countries.OfficialName).
	OfficialName value.Nullable[string]
	// Capital binds the persisted Country.Capital column.
	// Example: orm.SetNull(Countries.Capital).
	Capital value.Nullable[string]
	// Region binds the persisted Country.Region column.
	// Example: orm.SetNull(Countries.Region).
	Region value.Nullable[string]
	// Subregion binds the persisted Country.Subregion column.
	// Example: orm.SetNull(Countries.Subregion).
	Subregion value.Nullable[string]
	// Currencies binds the persisted Country.Currencies column.
	// Example: encoded, err := value.NewJSON(currencies); if err == nil { _ = orm.Set(Countries.Currencies, encoded) }.
	Currencies value.JSON[[]Currency]
	// PrimaryCurrencyCode binds the persisted Country.PrimaryCurrencyCode column.
	// Example: orm.SetNull(Countries.PrimaryCurrencyCode).
	PrimaryCurrencyCode value.Nullable[string]
	// CallingCode binds the persisted Country.CallingCode column.
	// Example: orm.SetNull(Countries.CallingCode).
	CallingCode value.Nullable[string]
	// CallingRoot binds the persisted Country.CallingRoot column.
	// Example: orm.SetNull(Countries.CallingRoot).
	CallingRoot value.Nullable[string]
	// CallingSuffixes binds the persisted Country.CallingSuffixes column.
	// Example: encoded, err := value.NewJSON(callingSuffixes); if err == nil { _ = orm.Set(Countries.CallingSuffixes, encoded) }.
	CallingSuffixes value.JSON[[]string]
	// TLDs binds the persisted Country.TLDs column.
	// Example: encoded, err := value.NewJSON([]string{".my"}); if err == nil { _ = orm.Set(Countries.TLDs, encoded) }.
	TLDs value.JSON[[]string] `foundry:"column=tlds"`
	// Timezones binds the persisted Country.Timezones column.
	// Example: encoded, err := value.NewJSON(timezones); if err == nil { _ = orm.Set(Countries.Timezones, encoded) }.
	Timezones value.JSON[[]string]
	// Latitude binds the persisted Country.Latitude column.
	// Example: orm.SetNull(Countries.Latitude).
	Latitude value.Nullable[float64]
	// Longitude binds the persisted Country.Longitude column.
	// Example: orm.SetNull(Countries.Longitude).
	Longitude value.Nullable[float64]
	// Independent binds the persisted Country.Independent column.
	// Example: orm.SetNull(Countries.Independent).
	Independent value.Nullable[bool]
	// UNMember binds the persisted Country.UNMember column.
	// Example: orm.SetNull(Countries.UNMember).
	UNMember value.Nullable[bool]
	// FlagEmoji binds the persisted Country.FlagEmoji column.
	// Example: orm.SetNull(Countries.FlagEmoji).
	FlagEmoji value.Nullable[string]
	// Status binds the persisted Country.Status column.
	// Example: orm.Set(Countries.Status, status).
	Status Status
	// ConversionRate binds the persisted Country.ConversionRate column.
	// Example: orm.SetNull(Countries.ConversionRate).
	ConversionRate value.Nullable[decimal.Decimal] `foundry:"role=column"`
	// IsDefault binds the persisted Country.IsDefault column.
	// Example: orm.Set(Countries.IsDefault, isDefault).
	IsDefault bool `foundry:"role=column"`
	// ReferenceVersion binds the persisted Country.ReferenceVersion column.
	// Example: orm.Set(Countries.ReferenceVersion, referenceVersion).
	ReferenceVersion string
	// CreatedAt preserves an explicit creation time; omitted inserts use the application clock.
	// Updates do not automatically change it. Example: Country{CreatedAt: createdAt}.
	//
	// CreatedAt preserves an explicitly assigned creation time; when omitted on insert, the ORM uses the application clock.
	// The insert default is applied after before hooks and before field mutators; updates do not automatically change it.
	// Example: orm.Set(Countries.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is replaced by the application clock on writes, after before hooks and before field mutators.
	// Explicit assignments do not override this management. Example: orm.Desc(Countries.UpdatedAt).
	//
	// UpdatedAt is managed: writes replace assigned or omitted values with the application clock after before hooks and before field mutators.
	// Example: orm.Desc(Countries.UpdatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
}

func invalid() error { return safeerror.NewFault(fault.Invalid, "invalid country reference data") }
