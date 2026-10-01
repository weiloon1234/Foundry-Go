package translations

import (
	"context"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Resolved struct {
	Locale i18n.LocaleID
	Text   string
}

// Values is an immutable field snapshot. Empty strings are present translations,
// not missing values. Resolve follows i18n.LocaleSet.Fallbacks: requested, its
// supported regional parents (en-GB → en), the default, then lexicographic
// supported locale order. UI catalogs share the same parent-locale rule.
type Values struct {
	locales i18n.LocaleSet
	values  map[i18n.LocaleID]string
}

func (v Values) Exact(locale i18n.LocaleID) (value.Optional[string], error) {
	if v.values == nil || !v.locales.Contains(locale) {
		return value.Optional[string]{}, invalid()
	}
	text, ok := v.values[locale]
	if !ok {
		return value.Optional[string]{}, nil
	}
	return value.Set(text), nil
}
func (v Values) Resolve(locale i18n.LocaleID) (value.Optional[Resolved], error) {
	if v.values == nil {
		return value.Optional[Resolved]{}, invalid()
	}
	order, err := v.locales.Fallbacks(locale)
	if err != nil {
		return value.Optional[Resolved]{}, err
	}
	for _, id := range order {
		if text, ok := v.values[id]; ok {
			return value.Set(Resolved{Locale: id, Text: text}), nil
		}
	}
	return value.Optional[Resolved]{}, nil
}

// Entries returns an independent map for an explicit content export.
func (v Values) Entries() map[i18n.LocaleID]string {
	result := make(map[i18n.LocaleID]string, len(v.values))
	for k, text := range v.values {
		result[k] = text
	}
	return result
}
func (Values) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("translated field values")) }
func (Values) MarshalJSON() ([]byte, error) { return nil, invalid() }

type Batch[M any, K comparable] struct {
	field  Field[M, K]
	active map[string]bool
	values map[string]Values
	// subjects are the subject keys of the loaded owners, in load order.
	subjects []string
}

func (b Batch[M, K]) Get(owner model.Reference[M, K]) (Values, error) {
	if b.active == nil || b.values == nil {
		return Values{}, invalid()
	}
	subject, err := b.field.definition.owner.SubjectKey(owner)
	if err != nil {
		return Values{}, err
	}
	return b.get(subject)
}

// getAt returns the values of the i-th loaded owner with the subject key
// derived while loading.
func (b Batch[M, K]) getAt(i int) (Values, error) {
	if b.active == nil || b.values == nil || i < 0 || i >= len(b.subjects) {
		return Values{}, invalid()
	}
	return b.get(b.subjects[i])
}
func (b Batch[M, K]) get(subject string) (Values, error) {
	if !b.active[subject] {
		return Values{}, database.NotFound
	}
	return b.values[subject], nil
}

// Load uses one owner SELECT and keyset-paged translation SELECTs (MaxBatchRows
// rows per page) in the same read-only repeatable-read snapshot, so batches of
// owners × locales beyond one page load without partial data. Total rows are
// bounded by owners × supported locales; text is bounded by MaxBatchBytes.
// No per-model lazy queries or global cache.
func (f Field[M, K]) Load(ctx context.Context, m *Manager, owners []model.Reference[M, K]) (Batch[M, K], error) {
	if err := f.check(m); err != nil {
		return Batch[M, K]{}, err
	}
	var result Batch[M, K]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		result, err = f.loadIn(ctx, tx, m, owners)
		return err
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
}

// errBatchLimit reports a batch beyond MaxBatchRows pages' row bound or
// MaxBatchBytes; slot loading retries such a batch in smaller parts.
var errBatchLimit = fault.New(fault.Conflict, "translation batch exceeds its row or byte limit")

// loadIn reads one owner batch inside tx, which is either the store's own
// read-only snapshot or a savepoint joined to the caller's transaction.
func (f Field[M, K]) loadIn(ctx context.Context, tx *database.Tx, m *Manager, owners []model.Reference[M, K]) (Batch[M, K], error) {
	locales, err := i18n.SnapshotLocales(ctx, m.catalog)
	if err != nil {
		return Batch[M, K]{}, err
	}
	active, subjects, err := f.definition.owner.ActiveSubjects(ctx, tx, m.store.Registry(), owners)
	if err != nil {
		return Batch[M, K]{}, err
	}
	result := Batch[M, K]{field: f, active: active, values: make(map[string]Values, len(active)), subjects: subjects}
	if len(active) == 0 {
		return result, nil
	}
	checked, err := extensionrow.For(f.definition.owner, owners)
	if err != nil {
		return Batch[M, K]{}, err
	}
	keys := make([]string, 0, len(active))
	for key := range active {
		keys = append(keys, key)
		result.values[key] = Values{locales: locales, values: make(map[i18n.LocaleID]string)}
	}
	slices.Sort(keys)
	ids := locales.Locales()
	localeNames := make([]string, len(ids))
	for i, id := range ids {
		localeNames[i] = string(id)
	}
	fields := store.TranslationFields()
	base := store.QueryFoundryModelTranslations().Where(fields.Scope.Eq(f.definition.owner.Scope()), fields.SubjectKey.In(keys...), fields.Field.Eq(string(f.Name())), fields.Locale.In(localeNames...))
	// UNIQUE(scope,subject_key,field,locale) bounds the pages structurally.
	limit := len(keys) * len(ids)
	rows, bytes, after := 0, 0, ""
	for {
		page := base
		if after != "" {
			page = page.Where(fields.Key.Gt(after))
		}
		count := 0
		err := page.OrderBy(fields.Key.Asc()).Limit(MaxBatchRows).Each(ctx, tx, func(row store.Translation) error {
			count++
			rows++
			after = row.Key
			bytes += len(row.Value)
			if rows > limit || bytes > MaxBatchBytes {
				return errBatchLimit
			}
			if row.Field != string(f.Name()) || !validText(row.Value, f.definition.options.MaxBytes) {
				return invalid()
			}
			if err := validateOwnerRow(checked, row); err != nil {
				return err
			}
			values, ok := result.values[row.SubjectKey]
			if !ok || !locales.Contains(i18n.LocaleID(row.Locale)) {
				return invalid()
			}
			values.values[i18n.LocaleID(row.Locale)] = row.Value
			return nil
		})
		if err != nil {
			return Batch[M, K]{}, err
		}
		if count < MaxBatchRows {
			return result, nil
		}
	}
}
func (f Field[M, K]) Values(ctx context.Context, m *Manager, owner model.Reference[M, K]) (Values, error) {
	batch, err := f.Load(ctx, m, []model.Reference[M, K]{owner})
	if err != nil {
		return Values{}, err
	}
	return batch.Get(owner)
}
func (f Field[M, K]) Get(ctx context.Context, m *Manager, owner model.Reference[M, K], locale i18n.LocaleID) (value.Optional[string], error) {
	values, err := f.Values(ctx, m, owner)
	if err != nil {
		return value.Optional[string]{}, err
	}
	return values.Exact(locale)
}
func (f Field[M, K]) Resolve(ctx context.Context, m *Manager, owner model.Reference[M, K], locale i18n.LocaleID) (value.Optional[Resolved], error) {
	values, err := f.Values(ctx, m, owner)
	if err != nil {
		return value.Optional[Resolved]{}, err
	}
	return values.Resolve(locale)
}
