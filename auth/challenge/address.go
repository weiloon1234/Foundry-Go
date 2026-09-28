package challenge

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Address is an infrastructure boundary. Models retain their concrete key type
// in public flows; only persistence serializes identity.
type Address struct {
	Namespace keyspace.Namespace
	Provider  auth.ProviderName
	Model     string
	Purpose   Kind
}

func (a Address) Validate() error {
	if a.Purpose != ResetPassword && a.Purpose != VerifyEmail {
		return fault.New(fault.Invalid, "invalid challenge purpose")
	}
	return credential.ValidateAddress(a.Namespace, string(a.Purpose), string(a.Provider), a.Model)
}
func (a Address) Key() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return credential.AddressKey(a.Namespace, string(a.Purpose), string(a.Provider), a.Model)
}
func (a Address) SubjectKey(identity model.Identity) (string, error) {
	scope, err := a.Key()
	if err != nil {
		return "", err
	}
	return credential.SubjectKey(scope, a.Model, identity)
}
