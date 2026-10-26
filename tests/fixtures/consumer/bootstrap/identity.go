package bootstrap

import (
	"github.com/weiloon1234/Foundry-Go/model"
)

// FoundryIdentity preserves the declared stored identity for authentication.
func (m Member) FoundryIdentity() (model.Identity, error) {
	return MemberIdentity.ReferenceOf(m).Identity()
}

// FoundryIdentity preserves the declared stored identity for authentication.
func (m Operator) FoundryIdentity() (model.Identity, error) {
	return OperatorIdentity.ReferenceOf(m).Identity()
}
