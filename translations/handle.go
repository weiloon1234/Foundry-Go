package translations

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensionslot"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// TextHandle retains typed slot capabilities behind shared callback state.
// Example: slot.From(manager).Relation(Fields.Slot).
type TextHandle[M any, K comparable] struct {
	_    [0]*M
	_    [0]*K
	core *textHandleCore
}

// Callback pointers are created and consumed together here; they never cross the public API.
// Calls are synchronous: no callback retains a pointer to an argument or result.
type textHandleCore struct {
	bind         func(*Manager) *textHandleCore
	relation     func(unsafe.Pointer)
	validate     func() error
	describe     func() extensions.SlotDescription
	registration func() Registration
	name         func() Name
	options      func() Options
	rule         func() validation.Rule[map[i18n.LocaleID]string]
	mergeRule    func() validation.Rule[map[i18n.LocaleID]string]
	save         func(ctx context.Context, owner unsafe.Pointer, input map[i18n.LocaleID]string) error
	saveIn       func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, input map[i18n.LocaleID]string) error
	sync         func(ctx context.Context, owner unsafe.Pointer, input map[i18n.LocaleID]string) error
	syncIn       func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, input map[i18n.LocaleID]string) error
	forget       func(ctx context.Context, owner unsafe.Pointer, locale i18n.LocaleID) (bool, error)
	forgetIn     func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, locale i18n.LocaleID) (bool, error)
	clear        func(ctx context.Context, owner unsafe.Pointer) (int, error)
	clearIn      func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer) (int, error)
}

// TextOf declares a thin translated-text handle with the existing manager policy.
// Example: translations.TextOf(owner,"title",options,extensions.BindSlot(Articles.Key(),Articles.Title)).
func TextOf[M any, K comparable](owner extensions.Owner[M, K], name Name, options Options, binding extensions.SlotBinding[M, K, Text]) TextHandle[M, K] {
	return TextHandle[M, K]{core: wrapTextHandle(defineText(owner, name, options, binding))}
}

func wrapTextHandle[M any, K comparable](state textSlot[M, K]) *textHandleCore {
	return &textHandleCore{
		bind:         func(m *Manager) *textHandleCore { return wrapTextHandle(state.From(m)) },
		relation:     func(out unsafe.Pointer) { *(*extensionslot.Slot[M])(out) = state.loader },
		validate:     state.Validate,
		describe:     state.Describe,
		registration: state.Registration,
		name:         state.Name,
		options:      state.Options,
		rule:         state.Rule,
		mergeRule:    state.MergeRule,
		save: func(ctx context.Context, owner unsafe.Pointer, input map[i18n.LocaleID]string) error {
			return state.Save(ctx, *(*M)(owner), input)
		},
		saveIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, input map[i18n.LocaleID]string) error {
			return state.SaveIn(ctx, tx, *(*M)(owner), input)
		},
		sync: func(ctx context.Context, owner unsafe.Pointer, input map[i18n.LocaleID]string) error {
			return state.Sync(ctx, *(*M)(owner), input)
		},
		syncIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, input map[i18n.LocaleID]string) error {
			return state.SyncIn(ctx, tx, *(*M)(owner), input)
		},
		forget: func(ctx context.Context, owner unsafe.Pointer, locale i18n.LocaleID) (bool, error) {
			return state.Forget(ctx, *(*M)(owner), locale)
		},
		forgetIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer, locale i18n.LocaleID) (bool, error) {
			return state.ForgetIn(ctx, tx, *(*M)(owner), locale)
		},
		clear: func(ctx context.Context, owner unsafe.Pointer) (int, error) { return state.Clear(ctx, *(*M)(owner)) },
		clearIn: func(ctx context.Context, tx *database.Tx, owner unsafe.Pointer) (int, error) {
			return state.ClearIn(ctx, tx, *(*M)(owner))
		},
	}
}

// From returns a copy borrowing this family's manager; nil leaves it unbound.
// Example: slot.From(manager).
func (s TextHandle[M, K]) From(m *Manager) TextHandle[M, K] {
	if s.core != nil {
		s.core = s.core.bind(m)
	}
	return s
}

// Relation binds the loader to its concrete ORM field; mismatched fields fail validation.
// Example: Articles.Query().With(slot.Relation(Articles.Title)).
func (s TextHandle[M, K]) Relation(slot orm.Slot[M, Text]) orm.ExtensionRelation[M] {
	var source extensionslot.Slot[M]
	if s.core != nil {
		s.core.relation(unsafe.Pointer(&source))
	}
	return orm.FoundryExtension(slot, source, sqlowner.Queries())
}

// Validate checks the declaration and slot binding. Example: slot.Validate().
func (s TextHandle[M, K]) Validate() error {
	if s.core == nil {
		return invalid()
	}
	return s.core.validate()
}

// Describe returns an owned inspection snapshot. Example: slot.Describe().
func (s TextHandle[M, K]) Describe() extensions.SlotDescription {
	if s.core == nil {
		return *new(extensions.SlotDescription)
	}
	return s.core.describe()
}

// Registration returns the existing manager registration. Example: slot.Registration().
func (s TextHandle[M, K]) Registration() Registration {
	if s.core == nil {
		return *new(Registration)
	}
	return s.core.registration()
}

// Name returns the stored name. Example: slot.Name().
func (s TextHandle[M, K]) Name() Name {
	if s.core == nil {
		return *new(Name)
	}
	return s.core.name()
}

// Options returns the declared input bounds. Example: slot.Options().
func (s TextHandle[M, K]) Options() Options {
	if s.core == nil {
		return *new(Options)
	}
	return s.core.options()
}

// Rule validates complete input using the bound locale catalog. Example: slot.Rule().
func (s TextHandle[M, K]) Rule() validation.Rule[map[i18n.LocaleID]string] {
	if s.core == nil {
		return validation.Locales[map[i18n.LocaleID]string](nil, i18n.OptionalLocales)
	}
	return s.core.rule()
}

// MergeRule validates partial input using the bound locale catalog. Example: slot.MergeRule().
func (s TextHandle[M, K]) MergeRule() validation.Rule[map[i18n.LocaleID]string] {
	if s.core == nil {
		return validation.Locales[map[i18n.LocaleID]string](nil, i18n.OptionalLocales)
	}
	return s.core.mergeRule()
}

// Save merges supplied locales in the manager transaction.
// Example: slot.Save(ctx,article,input).
func (s TextHandle[M, K]) Save(ctx context.Context, owner M, input map[i18n.LocaleID]string) error {
	if s.core == nil {
		return invalid()
	}
	return s.core.save(ctx, unsafe.Pointer(&owner), input)
}

// SaveIn merges supplied locales in the manager transaction inside the caller transaction.
// Example: slot.SaveIn(ctx,tx,article,input).
func (s TextHandle[M, K]) SaveIn(ctx context.Context, tx *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	if s.core == nil {
		return invalid()
	}
	return s.core.saveIn(ctx, tx, unsafe.Pointer(&owner), input)
}

// Sync replaces supported locales and enforces required input.
// Example: slot.Sync(ctx,article,input).
func (s TextHandle[M, K]) Sync(ctx context.Context, owner M, input map[i18n.LocaleID]string) error {
	if s.core == nil {
		return invalid()
	}
	return s.core.sync(ctx, unsafe.Pointer(&owner), input)
}

// SyncIn replaces supported locales and enforces required input inside the caller transaction.
// Example: slot.SyncIn(ctx,tx,article,input).
func (s TextHandle[M, K]) SyncIn(ctx context.Context, tx *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	if s.core == nil {
		return invalid()
	}
	return s.core.syncIn(ctx, tx, unsafe.Pointer(&owner), input)
}

// Forget removes stored text and returns the removed amount.
// Example: slot.Forget(ctx,article,locale).
func (s TextHandle[M, K]) Forget(ctx context.Context, owner M, locale i18n.LocaleID) (bool, error) {
	if s.core == nil {
		return *new(bool), invalid()
	}
	return s.core.forget(ctx, unsafe.Pointer(&owner), locale)
}

// ForgetIn removes stored text inside the caller transaction.
// Example: slot.ForgetIn(ctx,tx,article,locale).
func (s TextHandle[M, K]) ForgetIn(ctx context.Context, tx *database.Tx, owner M, locale i18n.LocaleID) (bool, error) {
	if s.core == nil {
		return *new(bool), invalid()
	}
	return s.core.forgetIn(ctx, tx, unsafe.Pointer(&owner), locale)
}

// Clear removes stored text and returns the removed amount.
// Example: slot.Clear(ctx,article).
func (s TextHandle[M, K]) Clear(ctx context.Context, owner M) (int, error) {
	if s.core == nil {
		return *new(int), invalid()
	}
	return s.core.clear(ctx, unsafe.Pointer(&owner))
}

// ClearIn removes stored text inside the caller transaction.
// Example: slot.ClearIn(ctx,tx,article).
func (s TextHandle[M, K]) ClearIn(ctx context.Context, tx *database.Tx, owner M) (int, error) {
	if s.core == nil {
		return *new(int), invalid()
	}
	return s.core.clearIn(ctx, tx, unsafe.Pointer(&owner))
}

// Format omits retained runtime state. Example: fmt.Sprintf("%+v",slot).
func (TextHandle[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("translations slot handle"))
}
