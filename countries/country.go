// Package countries supplies an ordinary typed country model and an explicitly
// invoked, versioned reference-data seeder. Construction never seeds a database.
package countries

import (
	"strings"

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

// Country's natural primary key and all query/mutation fields are generated.
// Status, ConversionRate and IsDefault belong to the application; reference
// seeding never overwrites those fields on existing rows.
//
//foundry:model table=foundry_countries primary=ISO2
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
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func invalid() error { return fault.New(fault.Invalid, "invalid country reference data") }
