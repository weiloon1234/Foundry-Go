package countries

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/temporal"
)

const (
	BuiltinVersion  = "foundry-countries-v1"
	TimezoneVersion = "2026a"
	BuiltinCount    = 250
)

//go:embed data/seed.json
var builtinSeed []byte

//go:embed data/iana-zone-2026a.tab
var builtinZones string

// The input shape deliberately matches the pinned source data, including fields
// that are not persisted (capitals and assignment status). No HTTP fetch occurs.
type reference struct {
	ISO2                Code       `json:"iso2"`
	ISO3                string     `json:"iso3"`
	ISONumeric          *string    `json:"iso_numeric"`
	Name                string     `json:"name"`
	OfficialName        *string    `json:"official_name"`
	Capital             *string    `json:"capital"`
	Capitals            []string   `json:"capitals"`
	Region              *string    `json:"region"`
	Subregion           *string    `json:"subregion"`
	Currencies          []Currency `json:"currencies"`
	PrimaryCurrencyCode *string    `json:"primary_currency_code"`
	CallingCode         *string    `json:"calling_code"`
	CallingRoot         *string    `json:"calling_root"`
	CallingSuffixes     []string   `json:"calling_suffixes"`
	TLDs                []string   `json:"tlds"`
	Latitude            *float64   `json:"latitude"`
	Longitude           *float64   `json:"longitude"`
	Independent         *bool      `json:"independent"`
	UNMember            *bool      `json:"un_member"`
	FlagEmoji           *string    `json:"flag_emoji"`
	AssignmentStatus    string     `json:"status"`
	Timezones           []string   `json:"-"`
}

func loadReference() ([]reference, error) {
	decoder := json.NewDecoder(bytes.NewReader(builtinSeed))
	decoder.DisallowUnknownFields()
	var rows []reference
	if err := decoder.Decode(&rows); err != nil {
		return nil, invalid()
	}
	if err := decoder.Decode(new(any)); err != io.EOF || len(rows) != BuiltinCount {
		return nil, invalid()
	}
	zones, err := zoneMapping()
	if err != nil {
		return nil, err
	}
	seen2, seen3 := map[Code]bool{}, map[string]bool{}
	for i := range rows {
		row := &rows[i]
		if err := row.validate(); err != nil {
			return nil, err
		}
		if seen2[row.ISO2] || seen3[row.ISO3] {
			return nil, invalid()
		}
		seen2[row.ISO2] = true
		seen3[row.ISO3] = true
		mapped, ok := zones[row.ISO2]
		if !ok && row.ISO2 != "BV" && row.ISO2 != "HM" {
			return nil, invalid()
		}
		if mapped == nil {
			mapped = []string{}
		}
		row.Timezones = mapped
		delete(zones, row.ISO2)
	}
	if len(zones) != 0 {
		return nil, invalid()
	}
	slices.SortFunc(rows, func(a, b reference) int { return strings.Compare(string(a.ISO2), string(b.ISO2)) })
	return rows, nil
}
func (r reference) validate() error {
	if r.ISO2.Validate() != nil || len(r.ISO3) != 3 || r.Name == "" || len(r.Name) > 1024 || r.Currencies == nil || r.CallingSuffixes == nil || r.TLDs == nil {
		return invalid()
	}
	for _, letter := range r.ISO3 {
		if letter < 'A' || letter > 'Z' {
			return invalid()
		}
	}
	if r.ISONumeric != nil {
		if len(*r.ISONumeric) != 3 {
			return invalid()
		}
		for _, digit := range *r.ISONumeric {
			if digit < '0' || digit > '9' {
				return invalid()
			}
		}
	}
	if !coordinate(r.Latitude, 90) || !coordinate(r.Longitude, 180) {
		return invalid()
	}
	seen := map[string]bool{}
	for _, currency := range r.Currencies {
		if len(currency.Code) != 3 || seen[currency.Code] {
			return invalid()
		}
		for _, c := range currency.Code {
			if c < 'A' || c > 'Z' {
				return invalid()
			}
		}
		seen[currency.Code] = true
		if units, ok := currency.MinorUnits.Get(); ok && (units < 0 || units > 9) {
			return invalid()
		}
	}
	if r.PrimaryCurrencyCode != nil && !seen[*r.PrimaryCurrencyCode] {
		return invalid()
	}
	return nil
}
func coordinate(v *float64, max float64) bool {
	return v == nil || !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= -max && *v <= max
}
func zoneMapping() (map[Code][]string, error) {
	zones := make(map[Code][]string)
	scanner := bufio.NewScanner(strings.NewReader(builtinZones))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) < 3 {
			return nil, invalid()
		}
		zone := columns[2]
		if _, err := temporal.LoadTimeZone(zone); err != nil {
			return nil, err
		}
		for _, country := range strings.Split(columns[0], ",") {
			code := Code(country)
			if code.Validate() != nil {
				return nil, invalid()
			}
			if !slices.Contains(zones[code], zone) {
				zones[code] = append(zones[code], zone)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// The pinned reference assigns XK the Europe/Belgrade rules; zone.tab does
	// not list this user-assigned alpha-2 code itself.
	if _, err := temporal.LoadTimeZone("Europe/Belgrade"); err != nil {
		return nil, err
	}
	zones["XK"] = []string{"Europe/Belgrade"}
	return zones, nil
}
