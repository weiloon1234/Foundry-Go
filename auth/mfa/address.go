package mfa

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Address is the persistence boundary. A model/provider shares one TOTP factor
// across its guards; guard names do not create independent factor enrollments.
type Address struct {
	Namespace keyspace.Namespace
	Provider  auth.ProviderName
	Model     string
}

func (a Address) Validate() error {
	return credential.ValidateAddress(a.Namespace, "mfa.totp", string(a.Provider), a.Model)
}
func (a Address) Key() (string, error) {
	return credential.AddressKey(a.Namespace, "mfa.totp", string(a.Provider), a.Model)
}
func (a Address) SubjectKey(identity model.Identity) (string, error) {
	scope, err := a.Key()
	if err != nil {
		return "", err
	}
	return credential.SubjectKey(scope, a.Model, identity)
}
