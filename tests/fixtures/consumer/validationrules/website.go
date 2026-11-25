package validationrules

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// WebsiteInput keeps optional website validation independent of registration.
//
//foundry:dto
type WebsiteInput struct {
	Website value.Optional[string] `json:"website,omitzero"`
}

// WebsiteRules accepts omission and validates a supplied URL.
func WebsiteRules() validation.Rule[WebsiteInput] {
	return validation.Rules(WebsiteInputValidationFields().Website, validation.Optional(validation.URL[string]()))
}
