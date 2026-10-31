// Package translations persists localized model content. It uses i18n's injected
// locale catalog while keeping model fields separate from UI/message keys.
package translations

import (
	"fmt"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Name identifies a translated field.
type Name string

// MaxBatchRows is the keyset page size of batch loads, not a result cap: a
// batch holds at most owners × supported locales rows. MaxBatchBytes bounds
// the text retained by one Load or All result.
const (
	MaxValueBytes     = 64 << 10
	MaxFieldsPerOwner = 64
	MaxRowsPerOwner   = MaxFieldsPerOwner * i18n.MaxLocales
	MaxAssignments    = 256
	MaxBatchRows      = 4096
	MaxBatchBytes     = 4 << 20
)

// Options bound one translated field. MaxBytes limits each locale's text; zero
// selects MaxValueBytes. Require selects the locales that slot input rules and
// exact synchronization demand; lower-level writes do not enforce it.
type Options struct {
	MaxBytes int
	Require  i18n.LocaleRequirement
}

type declarationID struct{ nonzero byte }

// Field is a translated field bound to a concrete model owner and key type.
type Field[M any, K comparable] struct{ definition *definition[M, K] }
type definition[M any, K comparable] struct {
	owner   extensions.Owner[M, K]
	name    Name
	options Options
	id      *declarationID
}

// Define declares an owner's translated field, using MaxValueBytes when its byte limit is zero.
func Define[M any, K comparable](owner extensions.Owner[M, K], name Name, options Options) Field[M, K] {
	if options.MaxBytes == 0 {
		options.MaxBytes = MaxValueBytes
	}
	return Field[M, K]{&definition[M, K]{owner: owner, name: name, options: options, id: &declarationID{}}}
}

// Name returns the field name, or an empty name for an undefined field.
func (f Field[M, K]) Name() Name {
	if f.definition == nil {
		return ""
	}
	return f.definition.name
}

// Validate checks the field name, byte limit, locale requirement, and owner.
func (f Field[M, K]) Validate() error {
	if f.definition == nil || !identifier.Semantic(string(f.Name())) || f.definition.options.MaxBytes < 1 || f.definition.options.MaxBytes > MaxValueBytes || f.definition.options.Require.Validate() != nil {
		return invalid()
	}
	return f.definition.owner.Validate()
}

// Format writes a fixed label without exposing the field declaration.
func (Field[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("translated model field")) }
func (f Field[M, K]) registrationKey() string {
	if f.definition == nil {
		return ""
	}
	return extensions.Digest(f.definition.owner.Scope(), string(f.Name()))
}

// Registration holds a translated field's registry identity, scope, and validation callback.
type Registration struct {
	key, scope string
	id         *declarationID
	validate   func(*extensions.Registry) error
	maxBytes   int
}

// Registration returns the field's owner-bound registration, or an empty declaration for an
// undefined field.
func (f Field[M, K]) Registration() Registration {
	if f.definition == nil {
		return Registration{}
	}
	return Registration{key: f.registrationKey(), scope: f.definition.owner.Scope(), id: f.definition.id, maxBytes: f.definition.options.MaxBytes, validate: func(r *extensions.Registry) error {
		if err := f.Validate(); err != nil {
			return err
		}
		return f.definition.owner.Check(r)
	}}
}

// Manager borrows both Store and catalog. Store owns actual operation lifetimes;
// every operation captures one supported/default locale snapshot.
type Manager struct {
	store   *extensions.Store
	catalog i18n.LocaleCatalog
	fields  map[string]Registration
}

// New validates and registers translated fields with an extension store and locale catalog,
// rejecting duplicates and excessive declarations.
func New(store *extensions.Store, catalog i18n.LocaleCatalog, fields ...Registration) (*Manager, error) {
	if err := store.Validate(); err != nil {
		return nil, err
	}
	if err := i18n.ValidateLocaleCatalog(catalog); err != nil {
		return nil, err
	}
	if len(fields) > 4096 {
		return nil, invalid()
	}
	m := &Manager{store: store, catalog: catalog, fields: make(map[string]Registration, len(fields))}
	counts := map[string]int{}
	for _, f := range fields {
		if f.id == nil || f.validate == nil {
			return nil, invalid()
		}
		if err := f.validate(store.Registry()); err != nil {
			return nil, err
		}
		if _, ok := m.fields[f.key]; ok {
			return nil, safeerror.NewFault(fault.Duplicate, "translated field already registered")
		}
		counts[f.scope]++
		if counts[f.scope] > MaxFieldsPerOwner {
			return nil, invalid()
		}
		m.fields[f.key] = f
	}
	return m, nil
}

// Validate checks manager initialization, locale catalog, and extension store.
func (m *Manager) Validate() error {
	if m == nil || m.fields == nil {
		return invalid()
	}
	if err := i18n.ValidateLocaleCatalog(m.catalog); err != nil {
		return err
	}
	return m.store.Validate()
}
func (f Field[M, K]) check(m *Manager) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if m.fields[f.registrationKey()].id != f.definition.id {
		return invalid()
	}
	return f.definition.owner.Check(m.store.Registry())
}
func validText(s string, limit int) bool {
	if len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == 0 {
			return false
		}
	}
	return true
}
func invalid() error {
	return safeerror.NewFault(fault.Invalid, "invalid model translation declaration or data")
}
