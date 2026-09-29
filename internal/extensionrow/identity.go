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

// Adopt verifies a row recorded under an earlier scope of its registered owner
// and returns the owner's current subject for it. The row's own key must match
// its recorded scope and subject key, and the stored key must round trip
// through the current codec to the same subject key; inconsistent rows fail
// closed. A row is adopted only when both its recorded scope and its identity's
// model name belong to models the owner declares (see Registry.DeclaresModel);
// any other row is reported as not declared (false) and left in place.
func Adopt(registry *extensions.Registry, owner extensions.OwnerName, row Identity, parts ...string) (extensions.Subject, bool, error) {
	if row.Owner != string(owner) || !ValidKey(row.Scope) || !ValidKey(row.SubjectKey) {
		return extensions.Subject{}, false, invalid()
	}
	input := make([]string, 0, 2+len(parts))
	input = append(input, row.Scope, row.SubjectKey)
	input = append(input, parts...)
	if row.Key != extensions.Digest(input...) {
		return extensions.Subject{}, false, invalid()
	}
	identity, err := row.Identity.Decode()
	if err != nil {
		return extensions.Subject{}, false, err
	}
	if !registry.DeclaresModel(owner, identity.ModelName()) || !registry.DeclaresScope(owner, row.Scope) {
		return extensions.Subject{}, false, nil
	}
	subject, err := registry.AdoptSubject(owner, identity)
	if err != nil {
		return extensions.Subject{}, false, err
	}
	if subject.Key != row.SubjectKey {
		return extensions.Subject{}, false, invalid()
	}
	return subject, true, nil
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
