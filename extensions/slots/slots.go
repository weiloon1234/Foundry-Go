// Package slots binds generated model extension slots to the translation,
// attachment and metadata managers. A model declares translations.Text,
// attachments.One/Many and metadata.Value fields; generation derives one
// Declaration per model, and applications bind a Runtime once per constructor.
package slots

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Runtime borrows the managers that bound slot descriptors use. A nil manager
// leaves slots of that kind unbound; using one reports fault.Missing. The
// application owns the managers' lifetimes; Runtime owns nothing.
type Runtime struct {
	Translations *translations.Manager
	Attachments  *attachments.Manager
	Metadata     *metadata.Manager
}

func (Runtime) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model extension runtime")) }

// Parts lists one model's slot registrations by kind, and each slot's
// inspection description in field order.
type Parts struct {
	Translations []translations.Registration
	Attachments  []attachments.Registration
	Metadata     []metadata.Registration
	Slots        []extensions.SlotDescription
}

func (p Parts) clone() Parts {
	result := Parts{Translations: slices.Clone(p.Translations), Attachments: slices.Clone(p.Attachments), Metadata: slices.Clone(p.Metadata), Slots: slices.Clone(p.Slots)}
	for i := range result.Slots {
		result.Slots[i].Accepted = slices.Clone(result.Slots[i].Accepted)
		result.Slots[i].Variants = slices.Clone(result.Slots[i].Variants)
	}
	return result
}

// Description is an owned inspection snapshot of one model's declaration.
type Description struct {
	Owner extensions.OwnerDescription  `json:"owner"`
	Slots []extensions.SlotDescription `json:"slots"`
}

// ResolveRuntime supplies the runtime while an application is constructed.
type ResolveRuntime func(foundation.Resolver) (Runtime, error)

// Installer registers a generated model's deletion-cleanup observer on a pool.
type Installer func(*foundation.Registrar, foundation.Key[*database.DB], ResolveRuntime) error

// Declaration is one model's generated extension envelope: its owner, slot
// registrations by kind and deletion-cleanup observer. It is immutable and
// owns no global state.
type Declaration struct {
	owner   extensions.Declaration
	parts   Parts
	install Installer
}

// Declare is called by generated <Model>ExtensionDeclaration functions.
func Declare(owner extensions.Declaration, parts Parts, install Installer) Declaration {
	return Declaration{owner: owner, parts: parts.clone(), install: install}
}

// Owner is the owner registration for the extension registry.
func (d Declaration) Owner() extensions.Declaration { return d.owner }

// Parts returns an owned copy of the slot registrations.
func (d Declaration) Parts() Parts { return d.parts.clone() }

// Describe returns an owned snapshot for declaration inspection. It performs
// no I/O and resolves no service.
func (d Declaration) Describe() Description {
	return Description{Owner: d.owner.Describe(), Slots: d.parts.clone().Slots}
}

// Validate checks that the declaration came from generated code.
func (d Declaration) Validate() error {
	if d.owner.Name() == "" || d.install == nil {
		return fault.New(fault.Invalid, "model extension declaration requires generated owner and cleanup registration")
	}
	return nil
}

// Register installs the deletion-cleanup observer on the pool that writes the
// owning model. The pool must be the extension store's database, because
// cleanup joins the owner's deletion transaction.
func (d Declaration) Register(r *foundation.Registrar, pool foundation.Key[*database.DB], resolve ResolveRuntime) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if resolve == nil {
		return fault.New(fault.Invalid, "model extension registration requires a runtime resolver")
	}
	return d.install(r, pool, resolve)
}

func (Declaration) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("model extension declaration"))
}

// ObserverName is the stable cleanup-observer name of model M, unique within
// a database across models and distinct from application observer names.
func ObserverName[M any]() (string, error) {
	typ := reflect.TypeFor[M]()
	if typ.Kind() != reflect.Struct || typ.Name() == "" || typ.PkgPath() == "" {
		return "", fault.New(fault.Invalid, "model extension observer requires a defined model struct")
	}
	digest := sha256.Sum256([]byte(typ.PkgPath() + "." + typ.Name()))
	return "foundry.extensions." + hex.EncodeToString(digest[:]), nil
}

// Cleanup removes one hard-deleted owner's extension data inside its deletion
// transaction. It runs the existing metadata, translation and attachment
// cleanup for every configured manager, so data written through the explicit
// APIs with the same owner is removed too. Soft deletion preserves data.
type Cleanup[M any] struct {
	run func(context.Context, *database.Tx, M, lifecycle.Operation) error
}

// NewCleanup binds the runtime once during construction. A slot kind whose
// manager is absent fails here instead of silently skipping cleanup.
func NewCleanup[M any, K comparable](runtime Runtime, owner extensions.Owner[M, K], parts Parts, reference func(M) model.Reference[M, K]) (Cleanup[M], error) {
	if reference == nil {
		return Cleanup[M]{}, fault.New(fault.Invalid, "model extension cleanup requires a reference function")
	}
	if err := owner.Validate(); err != nil {
		return Cleanup[M]{}, err
	}
	switch {
	case len(parts.Translations) > 0 && runtime.Translations == nil:
		return Cleanup[M]{}, missing(owner.Name(), "translations")
	case len(parts.Attachments) > 0 && runtime.Attachments == nil:
		return Cleanup[M]{}, missing(owner.Name(), "attachments")
	case len(parts.Metadata) > 0 && runtime.Metadata == nil:
		return Cleanup[M]{}, missing(owner.Name(), "metadata")
	}
	return Cleanup[M]{run: func(ctx context.Context, tx *database.Tx, before M, operation lifecycle.Operation) error {
		ref := reference(before)
		if runtime.Metadata != nil {
			if err := metadata.Cleanup(ctx, tx, runtime.Metadata, owner, ref, operation); err != nil {
				return err
			}
		}
		if runtime.Translations != nil {
			if err := translations.Cleanup(ctx, tx, runtime.Translations, owner, ref, operation); err != nil {
				return err
			}
		}
		if runtime.Attachments != nil {
			return attachments.Cleanup(ctx, tx, runtime.Attachments, owner, ref, operation, nil)
		}
		return nil
	}}, nil
}

// Deleted is the generated observer's Deleted callback body. It also runs for
// force deletion; soft deletion returns without changes.
func (c Cleanup[M]) Deleted(ctx context.Context, tx *database.Tx, before value.Optional[M], operation value.Optional[lifecycle.Operation]) error {
	if c.run == nil {
		return fault.New(fault.Invalid, "model extension cleanup is not initialized")
	}
	deleted, ok := before.Get()
	if !ok {
		return fault.New(fault.Missing, "model extension cleanup requires the deleted model")
	}
	current, ok := operation.Get()
	if !ok {
		return fault.New(fault.Missing, "model extension cleanup requires a lifecycle operation")
	}
	return c.run(ctx, tx, deleted, current)
}

func missing(owner extensions.OwnerName, kind string) error {
	return fault.New(fault.Missing, "model extension owner "+string(owner)+" declares "+kind+" slots, but the "+kind+" feature is not configured")
}
