package translations

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Assignment[M any, K comparable] struct {
	field  Field[M, K]
	locale i18n.LocaleID
	text   string
}

func (f Field[M, K]) SetValue(locale i18n.LocaleID, text string) Assignment[M, K] {
	return Assignment[M, K]{field: f, locale: locale, text: text}
}
func (f Field[M, K]) Set(ctx context.Context, m *Manager, owner model.Reference[M, K], locale i18n.LocaleID, text string) error {
	return Set(ctx, m, owner, f.SetValue(locale, text))
}
func (f Field[M, K]) SetIn(ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K], locale i18n.LocaleID, text string) error {
	return SetIn(ctx, tx, m, owner, f.SetValue(locale, text))
}

// Set atomically writes multiple registered fields/locales of the same typed
// owner. All declarations and values are validated before any write.
func Set[M any, K comparable](ctx context.Context, m *Manager, owner model.Reference[M, K], assignments ...Assignment[M, K]) error {
	return set(ctx, nil, m, owner, assignments)
}
func SetIn[M any, K comparable](ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K], assignments ...Assignment[M, K]) error {
	if tx == nil {
		return invalid()
	}
	return set(ctx, tx, m, owner, assignments)
}
func set[M any, K comparable](ctx context.Context, outer *database.Tx, m *Manager, reference model.Reference[M, K], input []Assignment[M, K]) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if len(input) == 0 || len(input) > MaxAssignments {
		return invalid()
	}
	assignments := slices.Clone(input)
	for _, a := range assignments {
		if err := a.field.check(m); err != nil {
			return err
		}
		if a.locale.Validate() != nil || !validText(a.text, a.field.definition.options.MaxBytes) {
			return invalid()
		}
	}
	owner := assignments[0].field.definition.owner
	run := func(ctx context.Context, tx *database.Tx) error {
		locales, err := i18n.SnapshotLocales(ctx, m.catalog)
		if err != nil {
			return err
		}
		subject, err := owner.Subject(reference)
		if err != nil {
			return err
		}
		keys := make([]string, len(assignments))
		seen := make(map[string]bool, len(assignments))
		for i, a := range assignments {
			if a.field.definition.owner.Scope() != owner.Scope() || !locales.Contains(a.locale) {
				return invalid()
			}
			keys[i] = extensions.Digest(subject.Scope, subject.Key, string(a.field.Name()), string(a.locale))
			if seen[keys[i]] {
				return fault.New(fault.Duplicate, "duplicate translated field and locale")
			}
			seen[keys[i]] = true
		}
		if _, err := owner.Lock(ctx, tx, m.store.Registry(), reference); err != nil {
			return err
		}
		q, f := store.QueryFoundryModelTranslations(), store.TranslationFields()
		existing, err := store.TranslationsIndex(q.Where(f.Key.In(keys...))).All(ctx, tx)
		if err != nil {
			return err
		}
		for _, row := range existing {
			if err := validateOwnerIndex(owner, row); err != nil {
				return err
			}
		}
		count, err := q.Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key)).Count(ctx, tx)
		if err != nil {
			return err
		}
		if count+int64(len(assignments)-len(existing)) > MaxRowsPerOwner {
			return fault.New(fault.Conflict, "model translation row limit reached")
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		// One set-based INSERT ... ON CONFLICT for every assignment
		// (MaxAssignments is below query.MaxInsertRows).
		drafts := make([]store.TranslationDraft, len(assignments))
		for i, a := range assignments {
			drafts[i] = store.TranslationDraft{}.SetKey(keys[i]).SetOwner(string(owner.Name())).SetScope(subject.Scope).SetSubjectKey(subject.Key).SetIdentity(subject.Identity).SetField(string(a.field.Name())).SetLocale(string(a.locale)).SetValue(a.text).SetCreatedAt(now).SetUpdatedAt(now)
		}
		_, err = q.UpsertMany(ctx, tx, drafts, query.OnConflict(f.Key).Update(f.Value, f.UpdatedAt))
		return err
	}
	if outer != nil {
		return m.store.Join(ctx, outer, run)
	}
	return m.store.Write(ctx, run)
}
func (f Field[M, K]) Forget(ctx context.Context, m *Manager, owner model.Reference[M, K], locale i18n.LocaleID) (bool, error) {
	if err := f.check(m); err != nil {
		return false, err
	}
	removed := false
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		locales, err := i18n.SnapshotLocales(ctx, m.catalog)
		if err != nil {
			return err
		}
		if !locales.Contains(locale) {
			return invalid()
		}
		subject, err := f.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
		if err != nil {
			return err
		}
		q, fields := store.QueryFoundryModelTranslations(), store.TranslationFields()
		rows, err := q.Where(fields.Key.Eq(extensions.Digest(subject.Scope, subject.Key, string(f.Name()), string(locale)))).DeleteEach(ctx, tx, 1)
		removed = len(rows) == 1
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}
