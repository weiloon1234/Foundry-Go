package attachments

import (
	"context"
	"crypto/sha256"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

const (
	MaxBatchFiles           = 4096
	MaxBatchPropertiesBytes = 4 << 20
)

type Batch[M any, K comparable] struct {
	collection Collection[M, K]
	active     map[string]extensions.Subject
	files      map[string][]Attachment[M, K]
}

func (b Batch[M, K]) Get(owner model.Reference[M, K]) ([]Attachment[M, K], error) {
	if b.active == nil || b.files == nil {
		return nil, invalid()
	}
	subject, err := b.collection.definition.owner.Subject(owner)
	if err != nil {
		return nil, err
	}
	if _, ok := b.active[subject.Key]; !ok {
		return nil, database.NotFound
	}
	return slices.Clone(b.files[subject.Key]), nil
}
func (c Collection[M, K]) Load(ctx context.Context, m *Manager, owners []model.Reference[M, K]) (Batch[M, K], error) {
	if err := c.check(m); err != nil {
		return Batch[M, K]{}, err
	}
	var result Batch[M, K]
	err := m.calls.Run(ctx, "attachment batch", func(ctx context.Context) error {
		var err error
		result, err = c.loadRows(ctx, m, owners, value.Optional[ID[M]]{})
		return err
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
}
func (c Collection[M, K]) loadRows(ctx context.Context, m *Manager, owners []model.Reference[M, K], only value.Optional[ID[M]]) (Batch[M, K], error) {
	var result Batch[M, K]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		result, err = c.scanRows(ctx, tx, m, owners, []string{locale}, only)
		return err
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
}

// scanRows is shared by exact-locale and all-locale reads. Owner visibility and
// attachment rows use the same repeatable-read transaction and bounded budget.
func (c Collection[M, K]) scanRows(ctx context.Context, tx *database.Tx, m *Manager, owners []model.Reference[M, K], locales []string, only value.Optional[ID[M]]) (Batch[M, K], error) {
	if id, ok := only.Get(); ok && id.IsZero() {
		return Batch[M, K]{}, invalid()
	}
	active, err := c.definition.owner.Active(ctx, tx, m.store.Registry(), owners)
	if err != nil {
		return Batch[M, K]{}, err
	}
	result := Batch[M, K]{collection: c, active: active, files: make(map[string][]Attachment[M, K], len(active))}
	if len(active) == 0 {
		return result, nil
	}
	keys := make([]string, 0, len(active))
	for key := range active {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	f := store.FileFields()
	q := store.QueryFoundryAttachments().Where(f.Scope.Eq(c.definition.owner.Scope()), f.SubjectKey.In(keys...), f.Collection.Eq(string(c.Name())), f.Locale.In(locales...), f.State.Eq(string(Ready)))
	if id, ok := only.Get(); ok {
		q = q.Where(f.ID.Eq(model.IDFromBytes[store.File](id.Bytes())))
	}
	count, bytes := 0, 0
	counts := make(map[string]map[string]int, len(active))
	err = q.OrderBy(f.SubjectKey.Asc(), f.Locale.Asc(), f.SortOrder.Asc(), f.ID.Asc()).Limit(MaxBatchFiles+1).Each(ctx, tx, func(row store.File) error {
		count++
		properties, err := row.Properties.Text()
		if err != nil {
			return err
		}
		bytes += len(properties)
		if count > MaxBatchFiles || bytes > MaxBatchPropertiesBytes {
			return fault.New(fault.Conflict, "attachment batch exceeds its row or property limit")
		}
		if _, ok := active[row.SubjectKey]; !ok {
			return invalid()
		}
		selected := c
		if c.definition.policy.Localized {
			selected = c.ForLocale(i18n.LocaleID(row.Locale))
		}
		file, err := selected.attachment(m, row)
		if err != nil {
			return err
		}
		if counts[row.SubjectKey] == nil {
			counts[row.SubjectKey] = make(map[string]int)
		}
		counts[row.SubjectKey][row.Locale]++
		if counts[row.SubjectKey][row.Locale] > c.definition.policy.MaxFiles {
			return fault.New(fault.Conflict, "stored attachment cardinality differs from its collection")
		}
		result.files[row.SubjectKey] = append(result.files[row.SubjectKey], file)
		return nil
	})
	if err != nil {
		return Batch[M, K]{}, err
	}
	return result, nil
}
func (c Collection[M, K]) List(ctx context.Context, m *Manager, owner model.Reference[M, K]) ([]Attachment[M, K], error) {
	batch, err := c.Load(ctx, m, []model.Reference[M, K]{owner})
	if err != nil {
		return nil, err
	}
	return batch.Get(owner)
}
func (c Collection[M, K]) First(ctx context.Context, m *Manager, owner model.Reference[M, K]) (value.Optional[Attachment[M, K]], error) {
	files, err := c.List(ctx, m, owner)
	if err != nil {
		return value.Optional[Attachment[M, K]]{}, err
	}
	if len(files) == 0 {
		return value.Optional[Attachment[M, K]]{}, nil
	}
	return value.Set(files[0]), nil
}
func (c Collection[M, K]) find(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (Attachment[M, K], error) {
	batch, err := c.loadRows(ctx, m, []model.Reference[M, K]{owner}, value.Set(id))
	if err != nil {
		return Attachment[M, K]{}, err
	}
	files, err := batch.Get(owner)
	if err != nil {
		return Attachment[M, K]{}, err
	}
	if len(files) != 1 {
		return Attachment[M, K]{}, database.NotFound
	}
	return files[0], nil
}
func (c Collection[M, K]) Find(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (Attachment[M, K], error) {
	if err := c.check(m); err != nil {
		return Attachment[M, K]{}, err
	}
	var result Attachment[M, K]
	err := m.calls.Run(ctx, "attachment lookup", func(ctx context.Context) error { var err error; result, err = c.find(ctx, m, owner, id); return err })
	if err != nil {
		return Attachment[M, K]{}, err
	}
	return result, nil
}

func readBytes[M any, K comparable](ctx context.Context, m *Manager, file Attachment[M, K], maximum int64) ([]byte, error) {
	if maximum < 1 || maximum > MaxUploadBytes || file.info.Size > maximum {
		return nil, storage.Failure(storage.LimitExceeded, storage.OpenOperation, storage.NotApplicable, nil)
	}
	disk, err := m.disks.Disk(file.disk)
	if err != nil {
		return nil, err
	}
	data, _, err := disk.ReadBytes(ctx, file.key, maximum, storage.ReadOptions{IfMatch: file.etag, Version: file.version})
	if err != nil {
		return nil, err
	}
	digest := storage.SHA256(sha256.Sum256(data))
	if int64(len(data)) != file.info.Size || digest != file.digest {
		return nil, storage.Failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	return data, nil
}
func (c Collection[M, K]) ReadBytes(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M], maximum int64) ([]byte, error) {
	if err := c.check(m); err != nil {
		return nil, err
	}
	if maximum < 1 || maximum > MaxUploadBytes {
		return nil, storage.Failure(storage.LimitExceeded, storage.OpenOperation, storage.NotApplicable, nil)
	}
	var result []byte
	err := m.calls.Run(ctx, "attachment read", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		result, err = readBytes(ctx, m, file, maximum)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (c Collection[M, K]) Image(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M], plan imaging.Plan) (imaging.Result, error) {
	if err := c.check(m); err != nil {
		return imaging.Result{}, err
	}
	if err := m.image.Validate(); err != nil {
		return imaging.Result{}, err
	}
	if err := plan.Validate(); err != nil {
		return imaging.Result{}, err
	}
	var result imaging.Result
	err := m.calls.Run(ctx, "attachment image", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		data, err := readBytes(ctx, m, file, min(MaxUploadBytes, m.image.Limits().InputBytes))
		if err != nil {
			return err
		}
		result, err = m.image.ProcessBytes(ctx, data, plan)
		return err
	})
	if err != nil {
		return imaging.Result{}, err
	}
	return result, nil
}
func (c Collection[M, K]) PublicURL(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (string, error) {
	if err := c.check(m); err != nil {
		return "", err
	}
	var result string
	err := m.calls.Run(ctx, "attachment public URL", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		disk, err := m.disks.Disk(file.disk)
		if err != nil {
			return err
		}
		if _, err := disk.Stat(ctx, file.key, storage.ReadOptions{IfMatch: file.etag}); err != nil {
			return err
		}
		result, err = disk.PublicURL(ctx, file.key)
		return err
	})
	if err != nil {
		return "", err
	}
	return result, nil
}
func (c Collection[M, K]) TemporaryURL(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M], expires time.Duration) (storage.TemporaryURL, error) {
	if err := c.check(m); err != nil {
		return storage.TemporaryURL{}, err
	}
	if err := (storage.LinkOptions{ExpiresIn: expires}).Validate(); err != nil {
		return storage.TemporaryURL{}, err
	}
	var result storage.TemporaryURL
	err := m.calls.Run(ctx, "attachment temporary URL", func(ctx context.Context) error {
		file, err := c.find(ctx, m, owner, id)
		if err != nil {
			return err
		}
		disk, err := m.disks.Disk(file.disk)
		if err != nil {
			return err
		}
		if _, err := disk.Stat(ctx, file.key, storage.ReadOptions{IfMatch: file.etag, Version: file.version}); err != nil {
			return err
		}
		result, err = disk.TemporaryURL(ctx, file.key, storage.LinkOptions{ExpiresIn: expires, Version: file.version})
		return err
	})
	if err != nil {
		return storage.TemporaryURL{}, err
	}
	return result, nil
}
