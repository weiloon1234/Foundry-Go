package translations

import (
	"context"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Resolved struct {
	Locale i18n.LocaleID
	Text   string
}

// Values is an immutable field snapshot. Empty strings are present translations,
// not missing values. Resolve follows requested, default, then lexicographic
// supported locale order, matching the reference framework's stable fallback.
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
	active map[string]extensions.Subject
	values map[string]Values
}

func (b Batch[M, K]) Get(owner model.Reference[M, K]) (Values, error) {
	if b.active == nil || b.values == nil {
		return Values{}, invalid()
	}
	subject, err := b.field.definition.owner.Subject(owner)
	if err != nil {
		return Values{}, err
	}
	if _, ok := b.active[subject.Key]; !ok {
		return Values{}, database.NotFound
	}
	return b.values[subject.Key], nil
}

// Load uses one owner SELECT and one streaming translation SELECT in the same
// read-only repeatable-read snapshot. No per-model lazy queries or global cache.
// Bounds are explicit; split larger batches instead of returning partial data.
func (f Field[M, K]) Load(ctx context.Context, m *Manager, owners []model.Reference[M, K]) (Batch[M, K], error) {
	if err := f.check(m); err != nil {
		return Batch[M, K]{}, err
	}
	var result Batch[M, K]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		locales, err := i18n.SnapshotLocales(ctx, m.catalog)
		if err != nil {
			return err
		}
		active, err := f.definition.owner.Active(ctx, tx, m.store.Registry(), owners)
		if err != nil {
			return err
		}
		result = Batch[M, K]{field: f, active: active, values: make(map[string]Values, len(active))}
		if len(active) == 0 {
			return nil
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
		count, bytes := 0, 0
		return store.QueryFoundryModelTranslations().Where(fields.Scope.Eq(f.definition.owner.Scope()), fields.SubjectKey.In(keys...), fields.Field.Eq(string(f.Name())), fields.Locale.In(localeNames...)).OrderBy(fields.Key.Asc()).Limit(MaxBatchRows+1).Each(ctx, tx, func(row store.Translation) error {
			count++
			bytes += len(row.Value)
			if count > MaxBatchRows || bytes > MaxBatchBytes {
				return fault.New(fault.Conflict, "translation batch exceeds its row or byte limit")
			}
			if row.Field != string(f.Name()) || !validText(row.Value, f.definition.options.MaxBytes) {
				return invalid()
			}
			if err := validateOwnerRow(f.definition.owner, row); err != nil {
				return err
			}
			values, ok := result.values[row.SubjectKey]
			if !ok || !locales.Contains(i18n.LocaleID(row.Locale)) {
				return invalid()
			}
			values.values[i18n.LocaleID(row.Locale)] = row.Value
			return nil
		})
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
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
