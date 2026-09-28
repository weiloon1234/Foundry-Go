// Package settings persists values through registered, typed key declarations.
// Form presentation is separate from the value codec and grants no access rights.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionvalue"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Name string
type Version uint32
type Group string
type Kind string

const (
	Text          Kind = "text"
	Textarea      Kind = "textarea"
	Number        Kind = "number"
	Boolean       Kind = "boolean"
	Select        Kind = "select"
	Multiselect   Kind = "multiselect"
	Email         Kind = "email"
	URL           Kind = "url"
	Color         Kind = "color"
	Date          Kind = "date"
	Datetime      Kind = "datetime"
	File          Kind = "file"
	Image         Kind = "image"
	JSON          Kind = "json"
	Password      Kind = "password"
	Code          Kind = "code"
	MaxKeys            = 1000
	MaxValueBytes      = extensionvalue.MaxBytes
	MaxBatchBytes      = extensionvalue.MaxBatchBytes
)

func (k Kind) Validate() error {
	switch k {
	case Text, Textarea, Number, Boolean, Select, Multiselect, Email, URL, Color, Date, Datetime, File, Image, JSON, Password, Code:
		return nil
	}
	return invalid()
}

// Presentation describes an administrative form. Password is only a masked
// widget; settings storage does not encrypt values. Parameters are explicitly
// dynamic JSON object data for the application's chosen widget.
type Presentation struct {
	Kind        Kind
	Group       Group
	Label       string
	Description string
	Order       int32
	Public      bool
	Parameters  value.JSON[json.RawMessage]
}

func (p Presentation) normalized() (Presentation, error) {
	if p.Kind == "" {
		p.Kind = Text
	}
	if p.Group == "" {
		p.Group = "general"
	}
	if p.Parameters.IsZero() {
		var err error
		p.Parameters, err = value.ParseJSON[json.RawMessage](`{}`)
		if err != nil {
			return Presentation{}, err
		}
	}
	return p, p.Validate()
}
func (p Presentation) Validate() error {
	if err := p.Kind.Validate(); err != nil {
		return err
	}
	if !identifier.Semantic(string(p.Group)) || !validText(p.Label, 512) || !validText(p.Description, 8192) {
		return invalid()
	}
	raw, err := p.Parameters.Text()
	if err != nil {
		return err
	}
	if len(raw) > 16384 || len(raw) == 0 || raw[0] != '{' {
		return invalid()
	}
	return nil
}
func validText(s string, max int) bool {
	if len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == 0 {
			return false
		}
	}
	return true
}

type declarationID struct{ nonzero byte }
type Key[V any] struct{ definition *definition[V] }
type definition[V any] struct {
	name         Name
	version      Version
	codec        contract.JSON[V]
	presentation Presentation
	id           *declarationID
	err          error
}

func Define[V any](name Name, version Version, codec contract.JSON[V], presentation Presentation) Key[V] {
	p, err := presentation.normalized()
	return Key[V]{&definition[V]{name: name, version: version, codec: codec, presentation: p, id: &declarationID{}, err: err}}
}
func (k Key[V]) Name() Name {
	if k.definition == nil {
		return ""
	}
	return k.definition.name
}
func (k Key[V]) Version() Version {
	if k.definition == nil {
		return 0
	}
	return k.definition.version
}
func (k Key[V]) Validate() error {
	if k.definition == nil || k.definition.err != nil || !identifier.Semantic(string(k.Name())) || k.Version() == 0 {
		return invalid()
	}
	if err := k.definition.presentation.Validate(); err != nil {
		return err
	}
	return k.definition.codec.Validate()
}
func (Key[V]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("typed setting key")) }

type Registration struct {
	name     Name
	version  Version
	id       *declarationID
	validate func() error
	decode   func(context.Context, value.JSON[json.RawMessage]) error
}

func (k Key[V]) Registration() Registration {
	if k.definition == nil {
		return Registration{}
	}
	return Registration{name: k.Name(), version: k.Version(), id: k.definition.id, validate: k.Validate, decode: func(ctx context.Context, v value.JSON[json.RawMessage]) error {
		_, err := extensionvalue.Decode(ctx, k.definition.codec, v)
		return err
	}}
}

// Manager borrows the extension Store, which owns admission and shutdown.
type Manager struct {
	store *extensions.Store
	keys  map[Name]Registration
}

func New(store *extensions.Store, keys ...Registration) (*Manager, error) {
	if err := store.Validate(); err != nil {
		return nil, err
	}
	if len(keys) > MaxKeys {
		return nil, invalid()
	}
	m := &Manager{store: store, keys: make(map[Name]Registration, len(keys))}
	for _, key := range keys {
		if key.id == nil || key.validate == nil || key.decode == nil {
			return nil, invalid()
		}
		if err := key.validate(); err != nil {
			return nil, err
		}
		if _, ok := m.keys[key.name]; ok {
			return nil, fault.New(fault.Duplicate, "setting already registered")
		}
		m.keys[key.name] = key
	}
	return m, nil
}
func (m *Manager) Validate() error {
	if m == nil || m.keys == nil {
		return invalid()
	}
	return m.store.Validate()
}
func (k Key[V]) check(m *Manager) error {
	if err := k.Validate(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if m.keys[k.Name()].id != k.definition.id {
		return invalid()
	}
	return nil
}
func invalid() error { return fault.New(fault.Invalid, "invalid setting declaration or data") }
