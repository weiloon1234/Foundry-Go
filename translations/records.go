package translations

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type Record[M any] struct {
	field            Name
	locale           i18n.LocaleID
	text             string
	created, updated temporal.DateTime
	_                [0]*M
}

func (r Record[M]) Field() Name                  { return r.field }
func (r Record[M]) Locale() i18n.LocaleID        { return r.locale }
func (r Record[M]) Text() string                 { return r.text }
func (r Record[M]) CreatedAt() temporal.DateTime { return r.created }
func (r Record[M]) UpdatedAt() temporal.DateTime { return r.updated }
func (Record[M]) Format(s fmt.State, _ rune)     { _, _ = s.Write([]byte("model translation record")) }
func (Record[M]) MarshalJSON() ([]byte, error)   { return nil, invalid() }
func rowIdentity(row store.Translation) extensionrow.Identity {
	return extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}
}
func validRow(row store.Translation) bool {
	return identifier.Semantic(row.Field) && i18n.LocaleID(row.Locale).Validate() == nil && validText(row.Value, MaxValueBytes)
}
func validateOwnerRow[M any, K comparable](rows extensionrow.Rows[M, K], row store.Translation) error {
	if !validRow(row) {
		return invalid()
	}
	return rows.Validate(rowIdentity(row), row.Field, row.Locale)
}

// validateOwnerIndex checks an ownership/index projection without its text.
func validateOwnerIndex[M any, K comparable](rows extensionrow.Rows[M, K], row store.TranslationIndex) error {
	if !identifier.Semantic(row.Field) || i18n.LocaleID(row.Locale).Validate() != nil {
		return invalid()
	}
	return rows.Validate(extensionrow.Identity{Key: row.Key, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity}, row.Field, row.Locale)
}

// All deliberately includes retained translations for locales no longer present
// in the catalog, and unregistered fields, for administrative migration. It
// requires a currently active owner and never bypasses the row/byte limits.
func All[M any, K comparable](ctx context.Context, m *Manager, owner extensions.Owner[M, K], reference model.Reference[M, K]) ([]Record[M], error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if err := owner.Check(m.store.Registry()); err != nil {
		return nil, err
	}
	var result []Record[M]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		subject, err := owner.SubjectKey(reference)
		if err != nil {
			return err
		}
		active, err := owner.Active(ctx, tx, m.store.Registry(), []model.Reference[M, K]{reference})
		if err != nil {
			return err
		}
		if !active[subject] {
			return database.NotFound
		}
		rows, err := extensionrow.For(owner, []model.Reference[M, K]{reference})
		if err != nil {
			return err
		}
		f := store.TranslationFields()
		bytes := 0
		return store.QueryFoundryModelTranslations().Where(f.Scope.Eq(owner.Scope()), f.SubjectKey.Eq(subject)).OrderBy(f.Field.Asc(), f.Locale.Asc()).Limit(MaxRowsPerOwner+1).Each(ctx, tx, func(row store.Translation) error {
			bytes += len(row.Value)
			if len(result) >= MaxRowsPerOwner || bytes > MaxBatchBytes {
				return fault.New(fault.Conflict, "translation result exceeds its row or byte limit")
			}
			if err := validateOwnerRow(rows, row); err != nil {
				return err
			}
			result = append(result, Record[M]{field: Name(row.Field), locale: i18n.LocaleID(row.Locale), text: row.Value, created: row.CreatedAt, updated: row.UpdatedAt})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Matching creates a bounded materialized owner scope for one exact translation.
// The following model query owns fresh visibility and authorization constraints.
func (f Field[M, K]) Matching(ctx context.Context, m *Manager, locale i18n.LocaleID, text string) (query.Predicate[M], error) {
	if err := f.check(m); err != nil {
		return query.Predicate[M]{}, err
	}
	if !validText(text, f.definition.options.MaxBytes) {
		return query.Predicate[M]{}, invalid()
	}
	var keys []K
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		locales, err := i18n.SnapshotLocales(ctx, m.catalog)
		if err != nil {
			return err
		}
		if !locales.Contains(locale) {
			return invalid()
		}
		fields := store.TranslationFields()
		// The value hash index (000002_index_translation_values) serves the
		// equality; only ownership/index columns are selected, never the text.
		return store.TranslationsIndex(store.QueryFoundryModelTranslations().Where(fields.Scope.Eq(f.definition.owner.Scope()), fields.Field.Eq(string(f.Name())), fields.Locale.Eq(string(locale)), fields.Value.Eq(text)).OrderBy(fields.Key.Asc()).Limit(query.MaxIdentityBatch+1)).Each(ctx, tx, func(row store.TranslationIndex) error {
			if len(keys) >= query.MaxIdentityBatch {
				return fault.New(fault.Conflict, "translation matching scope exceeds its limit")
			}
			if err := validateOwnerIndex(extensionrow.Unknown(f.definition.owner), row); err != nil {
				return err
			}
			identity, err := row.Identity.Decode()
			if err != nil {
				return err
			}
			ref, err := f.definition.owner.Parse(identity)
			if err != nil {
				return err
			}
			keys = append(keys, ref.Key())
			return nil
		})
	})
	if err != nil {
		return query.Predicate[M]{}, err
	}
	return f.definition.owner.QueryScope(keys...), nil
}
func (f Field[M, K]) Clear(ctx context.Context, m *Manager, reference model.Reference[M, K]) (int, error) {
	if err := f.check(m); err != nil {
		return 0, err
	}
	count := 0
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		count, err = f.clear(ctx, tx, m, reference)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// clear removes every locale of the field after locking the owner in tx.
func (f Field[M, K]) clear(ctx context.Context, tx *database.Tx, m *Manager, reference model.Reference[M, K]) (int, error) {
	subject, err := f.definition.owner.Lock(ctx, tx, m.store.Registry(), reference)
	if err != nil {
		return 0, err
	}
	fields := store.TranslationFields()
	return deleteRows(ctx, tx, store.QueryFoundryModelTranslations().Where(fields.Scope.Eq(subject.Scope), fields.SubjectKey.Eq(subject.Key), fields.Field.Eq(string(f.Name()))))
}
func DeleteAll[M any, K comparable](ctx context.Context, m *Manager, owner extensions.Owner[M, K], reference model.Reference[M, K]) (int, error) {
	if err := m.Validate(); err != nil {
		return 0, err
	}
	count := 0
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		subject, err := owner.Lock(ctx, tx, m.store.Registry(), reference)
		if err != nil {
			return err
		}
		count, err = deleteSubject(ctx, tx, subject)
		return err
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
func deleteSubject(ctx context.Context, tx *database.Tx, subject extensions.Subject) (int, error) {
	f := store.TranslationFields()
	return deleteRows(ctx, tx, store.QueryFoundryModelTranslations().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key)))
}

// deleteRows removes every row selected by q in one set-based DELETE USING,
// without hydrating translation text or issuing one statement per row. Callers
// lock the owner first (or run after its deletion); per-owner rows are bounded
// by MaxRowsPerOwner at write time.
func deleteRows(ctx context.Context, tx *database.Tx, q store.TranslationQuery) (int, error) {
	count, err := store.DeleteTranslationUsing(q, q).MatchKey(store.TranslationFields().Key.Value()).Exec(ctx, tx)
	if err != nil {
		return 0, err
	}
	return int(count), nil
}
func Cleanup[M any, K comparable](ctx context.Context, tx *database.Tx, m *Manager, owner extensions.Owner[M, K], reference model.Reference[M, K], operation lifecycle.Operation) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := owner.Check(m.store.Registry()); err != nil {
		return err
	}
	if operation == lifecycle.SoftDelete {
		return nil
	}
	if operation != lifecycle.Delete && operation != lifecycle.ForceDelete {
		return invalid()
	}
	subject, err := owner.Subject(reference)
	if err != nil {
		return err
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return err
	}
	return cleanupSubject(ctx, tx, m, owner.Name(), subject, identity)
}

// CleanupIdentity is Cleanup for an owner known by its registered name and
// persisted identity, such as a durable cleanup job's request. It joins tx and
// rejects an owner that still exists.
func CleanupIdentity(ctx context.Context, tx *database.Tx, m *Manager, owner extensions.OwnerName, identity model.Identity) error {
	if err := m.Validate(); err != nil {
		return err
	}
	subject, err := m.store.Registry().Subject(owner, identity)
	if err != nil {
		return err
	}
	return cleanupSubject(ctx, tx, m, owner, subject, identity)
}
func cleanupSubject(ctx context.Context, tx *database.Tx, m *Manager, owner extensions.OwnerName, subject extensions.Subject, identity model.Identity) error {
	return m.store.Join(ctx, tx, func(ctx context.Context, tx *database.Tx) error {
		retained, err := m.store.Registry().RetainedSubjects(ctx, tx, owner, []model.Identity{identity})
		if err != nil {
			return err
		}
		if retained[subject.Key] {
			return fault.New(fault.Conflict, "translation cleanup requires a deleted owner")
		}
		_, err = deleteSubject(ctx, tx, subject)
		return err
	})
}
