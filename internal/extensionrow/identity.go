// Package extensionrow validates persisted model-extension identities through
// the same typed owner descriptors that created them.
package extensionrow

import (
	"encoding/hex"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
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
func (r Identity) matches(owner extensions.OwnerName, scope, subject string, parts ...string) error {
	input := make([]string, 0, 2+len(parts))
	input = append(input, scope, subject)
	input = append(input, parts...)
	if r.Owner != string(owner) || r.Scope != scope || r.SubjectKey != subject || r.Key != extensions.Digest(input...) {
		return invalid()
	}
	return nil
}

// validate checks that row belongs to owner's current scope and that its
// stored identity derives its subject key and row key. Only the subject key
// is derived; nothing is snapshotted.
func validate[M any, K comparable](owner extensions.Owner[M, K], row Identity, parts ...string) error {
	identity, err := row.Identity.Decode()
	if err != nil {
		return err
	}
	ref, err := owner.Parse(identity)
	if err != nil {
		return err
	}
	subject, err := owner.SubjectKey(ref)
	if err != nil {
		return err
	}
	return row.matches(owner.Name(), owner.Scope(), subject, parts...)
}

// Rows validates persisted row identities: each row must belong to the
// owner's current scope, and its stored identity must derive its subject key
// and, with its parts such as a field and locale, its row key. A row whose
// stored identity is the canonical identity of the owner its subject key
// names needs no re-derivation, because that identity derived the key; any
// other row, such as one recorded under a declared storage model, is
// validated in full. Construction performs no I/O.
type Rows[M any, K comparable] struct {
	owner    extensions.Owner[M, K]
	expected map[string]string
}

// For prepares validation of rows read for references, a bounded batch.
func For[M any, K comparable](owner extensions.Owner[M, K], references []model.Reference[M, K]) (Rows[M, K], error) {
	if len(references) > query.MaxIdentityBatch {
		return Rows[M, K]{}, invalid()
	}
	rows := Rows[M, K]{owner: owner, expected: make(map[string]string, len(references))}
	for _, reference := range references {
		subject, err := owner.SubjectKey(reference)
		if err != nil {
			return Rows[M, K]{}, err
		}
		identity, err := reference.Identity()
		if err != nil {
			return Rows[M, K]{}, err
		}
		// The canonical text a value.JSON[model.Identity] snapshot of the
		// identity holds; a scanned row's identity is in the same form.
		data, err := json.Marshal(identity)
		if err != nil {
			return Rows[M, K]{}, invalid()
		}
		canonical, _, err := jsonwire.Parse(data)
		if err != nil {
			return Rows[M, K]{}, err
		}
		rows.expected[subject] = canonical
	}
	return rows, nil
}

// ForSubject prepares validation of rows of one subject that owner derived,
// through Subject or Lock, such as the owner a write locked.
func ForSubject[M any, K comparable](owner extensions.Owner[M, K], subject extensions.Subject) Rows[M, K] {
	rows := Rows[M, K]{owner: owner, expected: map[string]string{}}
	if text, err := subject.Identity.Text(); err == nil && subject.Scope == owner.Scope() {
		rows.expected[subject.Key] = text
	}
	return rows
}

// Unknown prepares validation of rows whose owners are not known in advance,
// such as rows a search matched; each row is validated in full.
func Unknown[M any, K comparable](owner extensions.Owner[M, K]) Rows[M, K] {
	return Rows[M, K]{owner: owner}
}

// Validate checks one row with its key parts.
func (r Rows[M, K]) Validate(row Identity, parts ...string) error {
	if expected, ok := r.expected[row.SubjectKey]; ok {
		if stored, err := row.Identity.Text(); err == nil && stored == expected {
			return row.matches(r.owner.Name(), r.owner.Scope(), row.SubjectKey, parts...)
		}
	}
	return validate(r.owner, row, parts...)
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
	if err := row.matches(owner, subject.Scope, subject.Key, parts...); err != nil {
		return model.Identity{}, err
	}
	return identity, nil
}
func invalid() error { return fault.New(fault.Invalid, "invalid persisted model extension identity") }
