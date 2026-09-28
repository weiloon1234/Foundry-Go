// Package attachments owns typed model collections and recoverable storage
// workflows. Applications declare policy once and pass typed owner references.
package attachments

import (
	"context"
	"fmt"
	"mime"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Name string
type Cardinality uint8

const (
	Single Cardinality = iota + 1
	Multiple
)
const (
	MaxUploadBytes     int64 = 64 << 20
	MaxCollectionFiles       = 256
	MaxOwnerIntents          = 4096
	MaxPropertiesBytes       = 64 << 10
)

type Policy struct {
	Disk           storage.Declaration
	Cardinality    Cardinality
	Localized      bool
	MaxFiles       int
	MaxBytes       int64
	MaxStoredBytes int64
	Accepted       []storage.MediaType
	AnyMedia       bool
	Image          value.Optional[imaging.Plan]
}

func (p Policy) normalized() Policy {
	p.Accepted = slices.Clone(p.Accepted)
	if p.MaxBytes == 0 {
		p.MaxBytes = 8 << 20
	}
	if p.MaxStoredBytes == 0 {
		p.MaxStoredBytes = p.MaxBytes
	}
	if p.MaxFiles == 0 {
		if p.Cardinality == Single {
			p.MaxFiles = 1
		} else {
			p.MaxFiles = 100
		}
	}
	return p
}
func (p Policy) Validate() error {
	if err := p.Disk.Validate(); err != nil {
		return err
	}
	if p.Cardinality != Single && p.Cardinality != Multiple || p.MaxFiles < 1 || p.MaxFiles > MaxCollectionFiles || p.Cardinality == Single && p.MaxFiles != 1 || p.MaxBytes < 1 || p.MaxBytes > MaxUploadBytes || p.MaxStoredBytes < 1 || p.MaxStoredBytes > MaxUploadBytes || len(p.Accepted) > 32 || p.AnyMedia && len(p.Accepted) > 0 {
		return invalid()
	}
	if plan, ok := p.Image.Get(); ok {
		if err := plan.Validate(); err != nil {
			return err
		}
	} else if len(p.Accepted) == 0 && !p.AnyMedia {
		return invalid()
	}
	seen := map[storage.MediaType]bool{}
	for _, media := range p.Accepted {
		if media.Validate() != nil || seen[media] {
			return invalid()
		}
		parsed, parameters, err := mime.ParseMediaType(string(media))
		if err != nil || len(parameters) != 0 || parsed != string(media) {
			return invalid()
		}
		seen[media] = true
	}
	return nil
}

// BeforeContext contains detected metadata, not the caller-owned reader or a
// mutable image buffer. The application can authorize through Owner.Key().
type BeforeContext[M any, K comparable] struct {
	Owner      model.Reference[M, K]
	Collection Name
	Locale     value.Optional[i18n.LocaleID]
	Upload     UploadInfo
}

// Hooks execute in declaration order. AfterStored runs inside the ownership
// transaction after storage succeeds and can veto publication. External effects
// from that hook must use an ordinary transactional outbox or after-commit hook.
type Hook[M any, K comparable] struct {
	Before      func(context.Context, BeforeContext[M, K]) error
	AfterStored func(context.Context, *database.Tx, Attachment[M, K]) error
}
type declarationID struct{ nonzero byte }
type Collection[M any, K comparable] struct {
	definition *definition[M, K]
	locale     value.Optional[i18n.LocaleID]
	queue      *Queue
}
type definition[M any, K comparable] struct {
	owner  extensions.Owner[M, K]
	name   Name
	policy Policy
	hooks  []Hook[M, K]
	id     *declarationID
}

func Define[M any, K comparable](owner extensions.Owner[M, K], name Name, policy Policy, hooks ...Hook[M, K]) Collection[M, K] {
	return Collection[M, K]{definition: &definition[M, K]{owner: owner, name: name, policy: policy.normalized(), hooks: slices.Clone(hooks), id: &declarationID{}}}
}
func (c Collection[M, K]) Name() Name {
	if c.definition == nil {
		return ""
	}
	return c.definition.name
}
func (c Collection[M, K]) Validate() error {
	if c.definition == nil || !identifier.Semantic(string(c.Name())) || len(c.definition.hooks) > 16 {
		return invalid()
	}
	if err := c.definition.owner.Validate(); err != nil {
		return err
	}
	for _, hook := range c.definition.hooks {
		if hook.Before == nil && hook.AfterStored == nil {
			return invalid()
		}
	}
	if locale, ok := c.locale.Get(); ok {
		if !c.definition.policy.Localized || locale.Validate() != nil {
			return invalid()
		}
	}
	if c.queue != nil {
		if err := c.queue.Validate(); err != nil {
			return err
		}
	}
	return c.definition.policy.Validate()
}
func (c Collection[M, K]) ForLocale(locale i18n.LocaleID) Collection[M, K] {
	c.locale = value.Set(locale)
	return c
}

// WithReconciliation binds ordinary job/outbox publication after service
// assembly without changing this collection's registered policy or identity.
func (c Collection[M, K]) WithReconciliation(queue Queue) Collection[M, K] {
	c.queue = &queue
	return c
}
func (Collection[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("typed attachment collection"))
}
func (c Collection[M, K]) registrationKey() string {
	if c.definition == nil {
		return ""
	}
	return extensions.Digest(c.definition.owner.Scope(), string(c.Name()))
}

type Registration struct {
	key      string
	id       *declarationID
	policy   Policy
	validate func(*extensions.Registry) error
}

func (c Collection[M, K]) Registration() Registration {
	if c.definition == nil || c.locale.IsSet() {
		return Registration{}
	}
	return Registration{key: c.registrationKey(), id: c.definition.id, policy: c.definition.policy, validate: func(r *extensions.Registry) error {
		if err := c.Validate(); err != nil {
			return err
		}
		return c.definition.owner.Check(r)
	}}
}
func (c Collection[M, K]) check(m *Manager) error {
	if err := c.checkRegistered(m); err != nil {
		return err
	}
	if c.definition.policy.Localized != c.locale.IsSet() {
		return invalid()
	}
	return nil
}
func (c Collection[M, K]) checkRegistered(m *Manager) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if m.collections[c.registrationKey()].id != c.definition.id {
		return invalid()
	}
	return c.definition.owner.Check(m.store.Registry())
}
func (c Collection[M, K]) localeName(ctx context.Context, m *Manager) (string, error) {
	if !c.definition.policy.Localized {
		return "", nil
	}
	locale, ok := c.locale.Get()
	if !ok {
		return "", invalid()
	}
	catalog, err := i18n.SnapshotLocales(ctx, m.locales)
	if err != nil {
		return "", err
	}
	if !catalog.Contains(locale) {
		return "", invalid()
	}
	return string(locale), nil
}
func invalid() error {
	return fault.New(fault.Invalid, "invalid attachment collection, operation or data")
}
