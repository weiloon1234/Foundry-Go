// Package extensionrow validates persisted model-extension identities through
// the same typed owner descriptors that created them.
package extensionrow

import (
	"encoding/hex"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Identity struct {
	Key, Owner, Scope, SubjectKey string
	Identity                      value.JSON[model.Identity]
}

func ValidKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	raw, err := hex.DecodeString(key)
	return err == nil && hex.EncodeToString(raw) == key
}
func (r Identity) matches(owner extensions.OwnerName, subject extensions.Subject, parts ...string) error {
	input := make([]string, 0, 2+len(parts))
	input = append(input, subject.Scope, subject.Key)
	input = append(input, parts...)
	if r.Owner != string(owner) || r.Scope != subject.Scope || r.SubjectKey != subject.Key || r.Key != extensions.Digest(input...) {
		return invalid()
	}
	return nil
}
func Validate[M any, K comparable](owner extensions.Owner[M, K], row Identity, parts ...string) error {
	identity, err := row.Identity.Decode()
	if err != nil {
		return err
	}
	ref, err := owner.Parse(identity)
	if err != nil {
		return err
	}
	subject, err := owner.Subject(ref)
	if err != nil {
		return err
	}
	return row.matches(owner.Name(), subject, parts...)
}
func Restore(registry *extensions.Registry, owner extensions.OwnerName, scope string, row Identity, parts ...string) (model.Identity, error) {
	identity, err := row.Identity.Decode()
	if err != nil {
		return model.Identity{}, err
	}
	subject, err := registry.Subject(owner, identity)
	if err != nil {
		return model.Identity{}, err
	}
	if subject.Scope != scope {
		return model.Identity{}, invalid()
	}
	if err := row.matches(owner, subject, parts...); err != nil {
		return model.Identity{}, err
	}
	return identity, nil
}
func invalid() error { return fault.New(fault.Invalid, "invalid persisted model extension identity") }
