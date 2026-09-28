package validationrules

import (
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

//foundry:dto
type AvailabilityWindow struct {
	Start temporal.DateTime `json:"start"`
	End   temporal.DateTime `json:"end"`
}

func WindowRules() validation.Rule[AvailabilityWindow] {
	fields := AvailabilityWindowValidationFields()
	return validation.BeforeField(fields.Start, fields.End)
}
