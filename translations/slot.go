package translations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Text is a model slot holding one translated field's values. The zero value
// is not loaded. Reading a slot performs no I/O; its values are an immutable
// snapshot taken with one supported/default locale catalog snapshot.
type Text struct {
	values Values
	loaded bool
}

// IsLoaded reports whether the slot was filled by a loader.
func (t Text) IsLoaded() bool { return t.loaded }

// Values returns the loaded snapshot and whether the slot was loaded.
func (t Text) Values() (Values, bool) { return t.values, t.loaded }

// Exact returns the text stored for exactly this supported locale. Empty text
// is a present value.
func (t Text) Exact(locale i18n.LocaleID) (value.Optional[string], error) {
	if !t.loaded {
		return value.Optional[string]{}, notLoaded()
	}
	return t.values.Exact(locale)
}

// Resolve follows the requested locale, its supported regional parents, the
// catalog default and then the remaining supported locales.
func (t Text) Resolve(locale i18n.LocaleID) (value.Optional[Resolved], error) {
	if !t.loaded {
		return value.Optional[Resolved]{}, notLoaded()
	}
	return t.values.Resolve(locale)
}

// ResolveRequest resolves the request's locale, i18n.RequestLocale, or the
// catalog default when the context carries none.
func (t Text) ResolveRequest(ctx context.Context) (value.Optional[Resolved], error) {
	if !t.loaded {
		return value.Optional[Resolved]{}, notLoaded()
	}
	locale, ok := i18n.RequestLocale(ctx)
	if !ok {
		locale = t.values.locales.Default()
	}
	return t.values.Resolve(locale)
}

func (Text) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("translated text slot")) }
func (Text) MarshalJSON() ([]byte, error) { return nil, invalid() }

func notLoaded() error {
	return fault.New(fault.Missing, "translated slot is not loaded; load it with With or Load")
}

// TextSlot binds one model's Text field to its translated-field declaration.
// Generated <Model>Extensions() constructs it; applications bind a manager once
// with From. A bound slot is an eager-loading relation for With and Load.
type TextSlot[M any, K comparable] struct {
	query.ExtensionSlot[M]
	field   Field[M, K]
	binding extensions.SlotBinding[M, K, Text]
	manager *Manager
}

// DefineText is the generated declaration boundary for a Text slot. It reuses
// Define, so the slot and the explicit Field API share one registration.
func DefineText[M any, K comparable](owner extensions.Owner[M, K], name Name, options Options, binding extensions.SlotBinding[M, K, Text]) TextSlot[M, K] {
	s := TextSlot[M, K]{field: Define(owner, name, options), binding: binding}
	s.ExtensionSlot = s.relation(nil)
	return s
}

// Field returns the underlying declaration for the explicit translation API.
func (s TextSlot[M, K]) Field() Field[M, K] { return s.field }

// Name is the stored field name.
func (s TextSlot[M, K]) Name() Name { return s.field.Name() }

// Options returns the declared text bounds and input requirement.
func (s TextSlot[M, K]) Options() Options {
	if s.field.definition == nil {
		return Options{}
	}
	return s.field.definition.options
}

// Describe returns the slot's inspection snapshot.
func (s TextSlot[M, K]) Describe() extensions.SlotDescription {
	options := s.Options()
	require := map[i18n.LocaleRequirement]string{i18n.DefaultLocale: "default", i18n.AllLocales: "all"}[options.Require]
	return extensions.SlotDescription{Field: s.binding.Field, Kind: "text", Name: string(s.Name()), Storage: "foundry_model_translations", MaxBytes: int64(options.MaxBytes), Require: require}
}

// From returns a copy that borrows the manager. A nil manager leaves the slot
// unbound; loading and writes then report fault.Missing.
func (s TextSlot[M, K]) From(m *Manager) TextSlot[M, K] {
	s.manager = m
	if m == nil {
		s.ExtensionSlot = s.relation(nil)
		return s
	}
	s.ExtensionSlot = s.relation(s.fetch)
	return s
}

// Validate checks the declaration and generated binding.
func (s TextSlot[M, K]) Validate() error {
	if err := s.binding.Validate(); err != nil {
		return err
	}
	return s.field.Validate()
}

// Registration is the manager registration of the underlying field, including
// the slot binding's validation.
func (s TextSlot[M, K]) Registration() Registration {
	registration := s.field.Registration()
	if err := s.binding.Validate(); err != nil && registration.validate != nil {
		registration.validate = func(*extensions.Registry) error { return err }
	}
	return registration
}

// bound returns the manager, or a Missing fault naming an unbound slot.
func (s TextSlot[M, K]) bound() (*Manager, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, s.unbound()
	}
	return s.manager, nil
}
func (s TextSlot[M, K]) unbound() error {
	return fault.New(fault.Missing, "translated slot "+s.binding.Field+" is not bound to a translations manager; bind it with <Model>Extensions().From(runtime)")
}

func (s TextSlot[M, K]) relation(fetch query.SlotFetch[M, Text]) query.ExtensionSlot[M] {
	table := ""
	if s.field.definition != nil {
		table = s.field.definition.owner.ModelName()
	}
	return query.NewExtensionSlot(query.ExtensionBinding[M, Text]{
		Name: s.binding.Field, Table: table, Get: s.binding.Get, Set: s.binding.Set,
		Loaded:  Text.IsLoaded,
		Count:   func(t Text) int { return boolCount(t.loaded) },
		Fetch:   fetch,
		Unbound: s.unbound(),
	})
}

// fetch loads one relation batch of parents, halving it when it exceeds the
// row or byte limit. Owners that are soft-deleted or no longer exist keep an
// unloaded slot.
func (s TextSlot[M, K]) fetch(ctx context.Context, executor database.Executor, parents []M) ([]Text, error) {
	m, err := s.bound()
	if err != nil {
		return nil, err
	}
	if err := s.field.check(m); err != nil {
		return nil, err
	}
	return extensions.LoadInParts(ctx, parents, len(parents), errBatchLimit, func(ctx context.Context, part []M) ([]Text, error) {
		references := make([]model.Reference[M, K], len(part))
		for i, parent := range part {
			references[i] = s.binding.Reference(parent)
		}
		var batch Batch[M, K]
		if err := m.store.ReadFor(ctx, executor, func(ctx context.Context, tx *database.Tx) error {
			var err error
			batch, err = s.field.loadIn(ctx, tx, m, references)
			return err
		}); err != nil {
			return nil, err
		}
		result := make([]Text, len(part))
		for i := range references {
			values, err := batch.getAt(i)
			if errors.Is(err, database.NotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			result[i] = Text{values: values, loaded: true}
		}
		return result, nil
	})
}

// SaveIn merges input into the stored text of owner inside tx: supplied
// locales are upserted in one statement and others stay unchanged. Every
// entry is validated before writing; empty text is a present value. A parent
// rollback rolls the write back. Empty input writes nothing.
func (s TextSlot[M, K]) SaveIn(ctx context.Context, tx *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	if tx == nil {
		return invalid()
	}
	return s.save(ctx, tx, owner, input)
}

// Save is SaveIn in the manager's own transaction.
func (s TextSlot[M, K]) Save(ctx context.Context, owner M, input map[i18n.LocaleID]string) error {
	return s.save(ctx, nil, owner, input)
}

// SyncIn makes the supported locales of this field exactly input inside tx:
// supplied locales are upserted and every other supported locale is removed.
// Options.Require is enforced: required locales need non-blank text, as Rule
// checks. Locales
// removed from the catalog keep their retained rows.
func (s TextSlot[M, K]) SyncIn(ctx context.Context, tx *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	if tx == nil {
		return invalid()
	}
	return s.sync(ctx, tx, owner, input)
}

// Sync is SyncIn in the manager's own transaction.
func (s TextSlot[M, K]) Sync(ctx context.Context, owner M, input map[i18n.LocaleID]string) error {
	return s.sync(ctx, nil, owner, input)
}

// ForgetIn removes one supported locale's text inside tx and reports whether
// a row existed.
func (s TextSlot[M, K]) ForgetIn(ctx context.Context, tx *database.Tx, owner M, locale i18n.LocaleID) (bool, error) {
	if tx == nil {
		return false, invalid()
	}
	m, err := s.bound()
	if err != nil {
		return false, err
	}
	if err := s.field.check(m); err != nil {
		return false, err
	}
	removed := false
	err = m.write(ctx, tx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		removed, err = s.field.forget(ctx, tx, m, s.binding.Reference(owner), locale)
		return err
	})
	return removed, err
}

// Forget is ForgetIn in the manager's own transaction.
func (s TextSlot[M, K]) Forget(ctx context.Context, owner M, locale i18n.LocaleID) (bool, error) {
	m, err := s.bound()
	if err != nil {
		return false, err
	}
	return s.field.Forget(ctx, m, s.binding.Reference(owner), locale)
}

// ClearIn removes every locale of this field inside tx and returns the count.
func (s TextSlot[M, K]) ClearIn(ctx context.Context, tx *database.Tx, owner M) (int, error) {
	if tx == nil {
		return 0, invalid()
	}
	m, err := s.bound()
	if err != nil {
		return 0, err
	}
	if err := s.field.check(m); err != nil {
		return 0, err
	}
	count := 0
	err = m.write(ctx, tx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		count, err = s.field.clear(ctx, tx, m, s.binding.Reference(owner))
		return err
	})
	return count, err
}

// Clear is ClearIn in the manager's own transaction.
func (s TextSlot[M, K]) Clear(ctx context.Context, owner M) (int, error) {
	m, err := s.bound()
	if err != nil {
		return 0, err
	}
	return s.field.Clear(ctx, m, s.binding.Reference(owner))
}

// Rule validates complete translated input, written with SyncIn, with one
// catalog snapshot per check: keys must be supported locales, Options.Require
// locales need nonempty text and each text is bounded by MaxBytes. Attach it
// to the request field, for example fields.Title.Rules(x.Title.Rule()). An
// unbound slot yields a rule that fails endpoint registration.
func (s TextSlot[M, K]) Rule() validation.Rule[map[i18n.LocaleID]string] {
	return s.rule(s.Options().Require)
}

// MergeRule validates partial translated input, written with SaveIn: keys must
// be supported locales and each text is bounded by MaxBytes. Options.Require
// does not apply, because a merge keeps the locales it does not mention.
func (s TextSlot[M, K]) MergeRule() validation.Rule[map[i18n.LocaleID]string] {
	return s.rule(i18n.OptionalLocales)
}

func (s TextSlot[M, K]) rule(require i18n.LocaleRequirement) validation.Rule[map[i18n.LocaleID]string] {
	var catalog i18n.LocaleCatalog
	if s.manager != nil {
		catalog = s.manager.catalog
	}
	// The value rules mirror the write's text validation: byte bound and no
	// NUL characters (JSON decoding already guarantees valid UTF-8).
	return validation.All(
		validation.Locales[map[i18n.LocaleID]string](catalog, require),
		validation.EachValue[map[i18n.LocaleID]string](validation.MaxBytes[string](s.Options().MaxBytes), validation.NotMatches[string]("\\x00")),
	)
}

func (s TextSlot[M, K]) save(ctx context.Context, outer *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	m, err := s.bound()
	if err != nil {
		return err
	}
	if len(input) == 0 {
		return nil
	}
	assignments, err := prepareAssignments(m, s.assignments(input))
	if err != nil {
		return err
	}
	return m.write(ctx, outer, func(ctx context.Context, tx *database.Tx) error {
		return upsert(ctx, tx, m, s.binding.Reference(owner), assignments)
	})
}

func (s TextSlot[M, K]) sync(ctx context.Context, outer *database.Tx, owner M, input map[i18n.LocaleID]string) error {
	m, err := s.bound()
	if err != nil {
		return err
	}
	if err := s.field.check(m); err != nil {
		return err
	}
	var assignments []Assignment[M, K]
	if len(input) > 0 {
		if assignments, err = prepareAssignments(m, s.assignments(input)); err != nil {
			return err
		}
	}
	reference := s.binding.Reference(owner)
	return m.write(ctx, outer, func(ctx context.Context, tx *database.Tx) error {
		locales, err := i18n.SnapshotLocales(ctx, m.catalog)
		if err != nil {
			return err
		}
		for _, locale := range s.Options().Require.Required(locales) {
			if strings.TrimSpace(input[locale]) == "" {
				return fault.New(fault.Invalid, "translated slot "+s.binding.Field+" requires text for locale "+string(locale))
			}
		}
		if len(assignments) > 0 {
			if err := upsert(ctx, tx, m, reference, assignments); err != nil {
				return err
			}
		} else if _, err := s.field.definition.owner.Lock(ctx, tx, m.store.Registry(), reference); err != nil {
			return err
		}
		var removed []string
		for _, locale := range locales.Locales() {
			if _, kept := input[locale]; !kept {
				removed = append(removed, string(locale))
			}
		}
		if len(removed) == 0 {
			return nil
		}
		owner := s.field.definition.owner
		subject, err := owner.SubjectKey(reference)
		if err != nil {
			return err
		}
		fields := store.TranslationFields()
		_, err = deleteRows(ctx, tx, store.QueryFoundryModelTranslations().Where(fields.Scope.Eq(owner.Scope()), fields.SubjectKey.Eq(subject), fields.Field.Eq(string(s.field.Name())), fields.Locale.In(removed...)))
		return err
	})
}

// assignments orders input by locale, so validation and writes are
// deterministic for Go's randomized map iteration.
func (s TextSlot[M, K]) assignments(input map[i18n.LocaleID]string) []Assignment[M, K] {
	locales := slices.Sorted(maps.Keys(input))
	result := make([]Assignment[M, K], len(locales))
	for i, locale := range locales {
		result[i] = s.field.SetValue(locale, input[locale])
	}
	return result
}

func boolCount(loaded bool) int {
	if loaded {
		return 1
	}
	return 0
}

func (TextSlot[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("translated text slot descriptor"))
}
