package token

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Address binds persisted tokens to an application, environment, model/provider
// and guard. It is an infrastructure boundary, not an application identity API.
type Address struct {
	Namespace keyspace.Namespace
	Guard     auth.GuardName
	Provider  auth.ProviderName
	Model     string
}

// Validate checks the namespace, guard, provider and model address components.
func (a Address) Validate() error {
	return credential.ValidateAddress(a.Namespace, string(a.Guard), string(a.Provider), a.Model)
}

// Key validates the address and returns its opaque persistence scope.
func (a Address) Key() (string, error) {
	return credential.AddressKey(a.Namespace, string(a.Guard), string(a.Provider), a.Model)
}

// SubjectKey returns the subject's opaque key within the validated address.
func (a Address) SubjectKey(identity model.Identity) (string, error) {
	scope, err := a.Key()
	if err != nil {
		return "", err
	}
	return credential.SubjectKey(scope, a.Model, identity)
}
