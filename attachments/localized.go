package attachments

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// LocalizedBatch captures one catalog and owner/attachment snapshot. Resolving
// many owners or changing the requested locale performs no further I/O.
type LocalizedBatch[M any, K comparable] struct {
	batch   Batch[M, K]
	catalog i18n.LocaleSet
}
type LocalizedFiles[M any, K comparable] struct {
	files   []Attachment[M, K]
	catalog i18n.LocaleSet
}
type Resolved[M any, K comparable] struct {
	locale i18n.LocaleID
	files  []Attachment[M, K]
}

func (r Resolved[M, K]) Locale() i18n.LocaleID     { return r.locale }
func (r Resolved[M, K]) Files() []Attachment[M, K] { return slices.Clone(r.files) }

func (b LocalizedBatch[M, K]) Get(owner model.Reference[M, K]) (LocalizedFiles[M, K], error) {
	files, err := b.batch.Get(owner)
	if err != nil {
		return LocalizedFiles[M, K]{}, err
	}
	return LocalizedFiles[M, K]{files: files, catalog: b.catalog}, nil
}
func (f LocalizedFiles[M, K]) List(locale i18n.LocaleID) ([]Attachment[M, K], error) {
	if err := f.catalog.Validate(); err != nil {
		return nil, err
	}
	if !f.catalog.Contains(locale) {
		return nil, invalid()
	}
	var result []Attachment[M, K]
	for _, file := range f.files {
		if found, ok := file.Locale().Get(); ok && found == locale {
			result = append(result, file)
		}
	}
	return result, nil
}

// Resolve chooses the first nonempty collection in requested/default/remaining
// lexical locale order. It never mixes files from different locale collections.
func (f LocalizedFiles[M, K]) Resolve(requested i18n.LocaleID) (value.Optional[Resolved[M, K]], error) {
	order, err := f.catalog.Fallbacks(requested)
	if err != nil {
		return value.Optional[Resolved[M, K]]{}, err
	}
	for _, locale := range order {
		files, err := f.List(locale)
		if err != nil {
			return value.Optional[Resolved[M, K]]{}, err
		}
		if len(files) > 0 {
			return value.Set(Resolved[M, K]{locale: locale, files: files}), nil
		}
	}
	return value.Optional[Resolved[M, K]]{}, nil
}
func (c Collection[M, K]) LoadLocalized(ctx context.Context, m *Manager, owners []model.Reference[M, K]) (LocalizedBatch[M, K], error) {
	if err := c.checkRegistered(m); err != nil {
		return LocalizedBatch[M, K]{}, err
	}
	if !c.definition.policy.Localized || c.locale.IsSet() {
		return LocalizedBatch[M, K]{}, invalid()
	}
	var result LocalizedBatch[M, K]
	err := m.calls.Run(ctx, "localized attachment batch", func(ctx context.Context) error {
		catalog, err := i18n.SnapshotLocales(ctx, m.locales)
		if err != nil {
			return err
		}
		locales := catalog.Locales()
		names := make([]string, len(locales))
		for i, locale := range locales {
			names[i] = string(locale)
		}
		return m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			batch, err := c.scanRows(ctx, tx, m, owners, names, value.Optional[ID[M]]{})
			if err != nil {
				return err
			}
			result = LocalizedBatch[M, K]{batch: batch, catalog: catalog}
			return nil
		})
	})
	if err != nil {
		return LocalizedBatch[M, K]{}, err
	}
	return result, nil
}
func (c Collection[M, K]) Resolve(ctx context.Context, m *Manager, owner model.Reference[M, K], requested i18n.LocaleID) (value.Optional[Resolved[M, K]], error) {
	if requested.Validate() != nil {
		return value.Optional[Resolved[M, K]]{}, invalid()
	}
	batch, err := c.LoadLocalized(ctx, m, []model.Reference[M, K]{owner})
	if err != nil {
		return value.Optional[Resolved[M, K]]{}, err
	}
	files, err := batch.Get(owner)
	if err != nil {
		return value.Optional[Resolved[M, K]]{}, err
	}
	return files.Resolve(requested)
}
