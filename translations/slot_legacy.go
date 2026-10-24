package translations

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// TextSlot retains the generated extension API during consumer migration.
// Example: slot.From(manager).
type TextSlot[M any, K comparable] struct {
	query.ExtensionSlot[M]
	core *textSlot[M, K]
}

func (v TextSlot[M, K]) state() textSlot[M, K] {
	if v.core == nil {
		return textSlot[M, K]{}
	}
	return *v.core
}

// DefineText preserves the generated declaration boundary. Example: DefineText(owner, name, options, binding).
func DefineText[M any, K comparable](owner extensions.Owner[M, K], name Name, options Options, binding extensions.SlotBinding[M, K, Text]) TextSlot[M, K] {
	state := defineText(owner, name, options, binding)
	return TextSlot[M, K]{query.FoundryExtensionSlot(state.loader, sqlowner.Queries()), &state}
}

// From returns a copy that borrows the manager. A nil manager leaves the slot
// unbound; loading and writes then report fault.Missing.
// Example: slot.From(manager).
func (v TextSlot[M, K]) From(m *Manager) TextSlot[M, K] {
	state := v.state().From(m)
	return TextSlot[M, K]{query.FoundryExtensionSlot(state.loader, sqlowner.Queries()), &state}
}

// Field returns the underlying declaration for the explicit translation API.
// Example: slot.Field().
func (v TextSlot[M, K]) Field() Field[M, K] { return v.state().Field() }

// Name is the stored field name.
// Example: slot.Name().
func (v TextSlot[M, K]) Name() Name { return v.state().Name() }

// Options returns the declared text bounds and input requirement.
// Example: slot.Options().
func (v TextSlot[M, K]) Options() Options { return v.state().Options() }

// Describe returns the slot's inspection snapshot.
// Example: slot.Describe().
func (v TextSlot[M, K]) Describe() extensions.SlotDescription { return v.state().Describe() }

// Validate checks the declaration and generated binding.
// Example: slot.Validate().
func (v TextSlot[M, K]) Validate() error { return v.state().Validate() }

// Registration is the manager registration of the underlying field, including
// the slot binding's validation.
// Example: slot.Registration().
func (v TextSlot[M, K]) Registration() Registration { return v.state().Registration() }

// SaveIn merges input into the stored text of owner inside tx: supplied
// locales are upserted in one statement and others stay unchanged. Every
// entry is validated before writing; empty text is a present value. A parent
// rollback rolls the write back. Empty input writes nothing.
// Example: slot.SaveIn(ctx, tx, owner, input).
func (v TextSlot[M, K]) SaveIn(ctx context.Context, tx *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	return v.state().SaveIn(ctx, tx, owner, input)
}

// Save is SaveIn in the manager's own transaction.
// Example: slot.Save(ctx, owner, input).
func (v TextSlot[M, K]) Save(ctx context.Context, owner M, input map[i18n.LocaleID]string) error {
	return v.state().Save(ctx, owner, input)
}

// SyncIn makes the supported locales of this field exactly input inside tx:
// supplied locales are upserted and every other supported locale is removed.
// Options.Require is enforced: required locales need non-blank text, as Rule
// checks. Locales
// removed from the catalog keep their retained rows.
// Example: slot.SyncIn(ctx, tx, owner, input).
func (v TextSlot[M, K]) SyncIn(ctx context.Context, tx *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	return v.state().SyncIn(ctx, tx, owner, input)
}

// Sync is SyncIn in the manager's own transaction.
// Example: slot.Sync(ctx, owner, input).
func (v TextSlot[M, K]) Sync(ctx context.Context, owner M, input map[i18n.LocaleID]string) error {
	return v.state().Sync(ctx, owner, input)
}

// ForgetIn removes one supported locale's text inside tx and reports whether
// a row existed.
// Example: slot.ForgetIn(ctx, tx, owner, locale).
func (v TextSlot[M, K]) ForgetIn(ctx context.Context, tx *database.Tx, owner M, locale i18n.LocaleID) (bool, error) {
	return v.state().ForgetIn(ctx, tx, owner, locale)
}

// Forget is ForgetIn in the manager's own transaction.
// Example: slot.Forget(ctx, owner, locale).
func (v TextSlot[M, K]) Forget(ctx context.Context, owner M, locale i18n.LocaleID) (bool, error) {
	return v.state().Forget(ctx, owner, locale)
}

// ClearIn removes every locale of this field inside tx and returns the count.
// Example: slot.ClearIn(ctx, tx, owner).
func (v TextSlot[M, K]) ClearIn(ctx context.Context, tx *database.Tx, owner M) (int, error) {
	return v.state().ClearIn(ctx, tx, owner)
}

// Clear is ClearIn in the manager's own transaction.
// Example: slot.Clear(ctx, owner).
func (v TextSlot[M, K]) Clear(ctx context.Context, owner M) (int, error) {
	return v.state().Clear(ctx, owner)
}

// Rule validates complete translated input, written with SyncIn, with one
// catalog snapshot per check: keys must be supported locales, Options.Require
// locales need nonempty text and each text is bounded by MaxBytes. Attach it
// to the request field, for example fields.Title.Rules(x.Title.Rule()). An
// unbound slot yields a rule that fails endpoint registration.
// Example: slot.Rule().
func (v TextSlot[M, K]) Rule() validation.Rule[map[i18n.LocaleID]string] { return v.state().Rule() }

// MergeRule validates partial translated input, written with SaveIn: keys must
// be supported locales and each text is bounded by MaxBytes. Options.Require
// does not apply, because a merge keeps the locales it does not mention.
// Example: slot.MergeRule().
func (v TextSlot[M, K]) MergeRule() validation.Rule[map[i18n.LocaleID]string] {
	return v.state().MergeRule()
}

// Format delegates to the shared slot state. Example: slot.Format(s, verb).
func (v TextSlot[M, K]) Format(s fmt.State, verb rune) { v.state().Format(s, verb) }
