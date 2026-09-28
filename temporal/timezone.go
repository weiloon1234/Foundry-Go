package temporal

import (
	"strings"
	"time"
	_ "time/tzdata" // Keep named zones available in minimal deployment images.
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ZoneName selects an explicit IANA zone, UTC or a fixed +/-HH:MM offset.
// It is distinct from a locale or arbitrary configuration string.
type ZoneName string

const UTC ZoneName = "UTC"

// Location validates the name and returns an owned location value. Modifying
// the returned value cannot change another application's selected timezone.
func (name ZoneName) Location() (*time.Location, error) {
	zone, err := ParseTimeZone(string(name))
	if err != nil {
		return nil, err
	}
	owned := *zone
	return &owned, nil
}

// ParseTimeZone resolves UTC, an IANA location or a fixed +/-HH:MM offset.
// Named locations use Go's configured timezone database. Empty and Local are
// rejected so application behavior does not silently depend on the host zone.
// The returned standard Go location works directly with DateTime.LocalIn and
// LocalDateTime.In. Fixed offsets must be smaller than 24 hours.
func ParseTimeZone(text string) (*time.Location, error) {
	if len(text) > 0 && (text[0] == '+' || text[0] == '-') {
		if len(text) != 6 || text[3] != ':' {
			return nil, invalidTimeZone()
		}
		for _, i := range [...]int{1, 2, 4, 5} {
			if text[i] < '0' || text[i] > '9' {
				return nil, invalidTimeZone()
			}
		}
		hour := int(text[1]-'0')*10 + int(text[2]-'0')
		minute := int(text[4]-'0')*10 + int(text[5]-'0')
		if hour > 23 || minute > 59 {
			return nil, invalidTimeZone()
		}
		offset := (hour*60 + minute) * 60
		if text[0] == '-' {
			offset = -offset
		}
		return time.FixedZone(text, offset), nil
	}
	return LoadTimeZone(text)
}

// LoadTimeZone resolves a bounded, explicit IANA name or UTC. It excludes fixed
// offsets, empty names and the process-dependent Local value. Use ParseTimeZone
// when a boundary accepts both named zones and fixed offsets.
func LoadTimeZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
		return nil, invalidTimeZone()
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalidTimeZone()
	}
	return zone, nil
}

func invalidTimeZone() error { return fault.New(fault.Invalid, "invalid timezone") }
