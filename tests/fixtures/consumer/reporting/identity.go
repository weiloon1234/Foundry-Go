package reporting

import (
	"github.com/weiloon1234/Foundry-Go/model"
)

// FoundryIdentity preserves the declared stored identity for authentication.
func (m Operator) FoundryIdentity() (model.Identity, error) {
	return OperatorIdentity.ReferenceOf(m).Identity()
}
