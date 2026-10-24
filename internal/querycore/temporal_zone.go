package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

const PostgresStandardTime uint8 = 1

// LoadTimeZone validates a named zone using Go's timezone database. Local and
// empty names are rejected to avoid process-specific timezone selection.
func LoadTimeZone(name string) (string, error) {
	if _, err := temporal.LoadTimeZone(name); err != nil {
		return "", fault.Wrap(fault.Invalid, "invalid query timezone", err)
	}
	return name, nil
}

func ZoneArgument(zone string) (string, error) {
	if zone == "" {
		return "", fault.New(fault.Invalid, "temporal operation requires a timezone")
	}
	return zone, nil
}
func ResolutionArgument(zone string, policy uint8) (string, error) {
	if policy != PostgresStandardTime {
		return "", fault.New(fault.Invalid, "local resolution requires an explicit supported DST policy")
	}
	return ZoneArgument(zone)
}
