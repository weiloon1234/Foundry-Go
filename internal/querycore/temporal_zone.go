package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

const PostgresStandardTime uint8 = 1

// LoadTimeZone validates a named zone using Go's timezone database. Local and
// empty names are rejected to avoid process-specific timezone selection.
func LoadTimeZone(name string) (string, error) {
	if _, err := temporal.LoadTimeZone(name); err != nil {
		return "", safeerror.WrapFailure(fault.Invalid, "invalid query timezone", err)
	}
	return name, nil
}

func ZoneArgument(zone string) (string, error) {
	if zone == "" {
		return "", safeerror.NewFault(fault.Invalid, "temporal operation requires a timezone")
	}
	return zone, nil
}
func ResolutionArgument(zone string, policy uint8) (string, error) {
	if policy != PostgresStandardTime {
		return "", safeerror.NewFault(fault.Invalid, "local resolution requires an explicit supported DST policy")
	}
	return ZoneArgument(zone)
}
