package querycore

import "github.com/weiloon1234/Foundry-Go/fault"

// ValidateSQLMapping owns the declaration rules shared by both model APIs.
func ValidateSQLMapping(model *ModelDefinition, column string, mutated bool) error {
	if mutated {
		return fault.New(fault.Invalid, "SQL mapping cannot bypass a Go field mutator; supply a literal draft input")
	}
	if model.Timestamps != nil && column == model.Timestamps.Updated {
		return fault.New(fault.Invalid, "SQL mapping cannot replace a managed update timestamp")
	}
	for _, c := range model.Columns {
		if c.Name == column && c.Encrypted {
			return fault.New(fault.Invalid, "SQL mapping cannot copy an encrypted envelope")
		}
	}
	return nil
}
