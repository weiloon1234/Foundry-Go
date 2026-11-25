package validationrules

import (
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// doc-example:begin window-input

//foundry:dto
type AvailabilityWindow struct {
	Start temporal.DateTime `json:"start"`
	End   temporal.DateTime `json:"end"`
}

// doc-example:end window-input

func WindowRules() validation.Rule[AvailabilityWindow] {
	// doc-example:begin compare-window-fields
	fields := AvailabilityWindowValidationFields()
	return validation.BeforeField(fields.Start, fields.End)
	// doc-example:end compare-window-fields
}
