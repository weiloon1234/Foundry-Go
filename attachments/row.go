package attachments

import (
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

const objectPrefix = "foundry-attachments/"

func objectKey(scope string, id model.ID[store.File]) (storage.ObjectKey, error) {
	return storage.ParseKey(objectPrefix + scope + "/" + id.String())
}
func operationID(id model.ID[store.File]) OperationID {
	return model.IDFromBytes[Operation](id.Bytes())
}
func fileID(id OperationID) model.ID[store.File] { return model.IDFromBytes[store.File](id.Bytes()) }

// validateRow treats persistence as a checked boundary before storage effects.
// A manually corrupted key must not cause deletion or signing of another file.
func (m *Manager) validateIndex(row store.FileIndex) (extensions.Subject, error) {
	if row.ID.IsZero() || !identifier.Semantic(row.Collection) || State(row.State).Validate() != nil || row.Locale != "" && i18n.LocaleID(row.Locale).Validate() != nil || storage.DiskID(row.Disk).Validate() != nil {
		return extensions.Subject{}, invalid()
	}
	identity, err := row.Identity.Decode()
	if err != nil {
		return extensions.Subject{}, err
	}
	subject, err := m.store.Registry().Subject(extensions.OwnerName(row.Owner), identity)
	if err != nil {
		return extensions.Subject{}, err
	}
	if subject.Scope != row.Scope || subject.Key != row.SubjectKey {
		return extensions.Subject{}, invalid()
	}
	expected, err := objectKey(subject.Scope, row.ID)
	if err != nil || expected.String() != row.ObjectKey {
		return extensions.Subject{}, invalid()
	}
	return subject, nil
}
func indexOf(row store.File) store.FileIndex {
	return store.FileIndex{ID: row.ID, Owner: row.Owner, Scope: row.Scope, SubjectKey: row.SubjectKey, Identity: row.Identity, Collection: row.Collection, Locale: row.Locale, State: row.State, Disk: row.Disk, ObjectKey: row.ObjectKey, UpdatedAt: row.UpdatedAt}
}
func (m *Manager) validateRow(row store.File) (extensions.Subject, error) {
	subject, err := m.validateIndex(indexOf(row))
	if err != nil {
		return extensions.Subject{}, err
	}
	if row.WriterID.IsZero() || row.Size < 0 || row.Size > MaxUploadBytes || row.Width < 0 || row.Height < 0 || row.Width > 65535 || row.Height > 65535 || (row.Width == 0) != (row.Height == 0) || row.SortOrder < 0 || row.Properties.IsZero() {
		return extensions.Subject{}, invalid()
	}
	if storage.MediaType(row.ContentType).Validate() != nil || storage.ETag(row.ETag).Validate() != nil || storage.VersionID(row.ObjectVersion).Validate() != nil {
		return extensions.Subject{}, invalid()
	}
	digest, err := storage.ParseSHA256(row.Digest)
	if err != nil || digest.String() != row.Digest {
		return extensions.Subject{}, invalid()
	}
	if row.State == string(Stored) || row.State == string(Ready) || row.State == string(CleanupPending) || row.State == string(Retained) {
		if row.ETag == "" {
			return extensions.Subject{}, invalid()
		}
	}
	if _, err := normalizeProperties(row.Properties); err != nil {
		return extensions.Subject{}, err
	}
	if !validFilename(row.OriginalName) {
		return extensions.Subject{}, invalid()
	}
	return subject, nil
}
func (c Collection[M, K]) attachment(m *Manager, row store.File) (Attachment[M, K], error) {
	expectedLocale := ""
	if locale, ok := c.locale.Get(); ok {
		expectedLocale = string(locale)
	}
	if row.Locale != expectedLocale {
		return Attachment[M, K]{}, invalid()
	}
	if row.State != string(Ready) || row.Collection != string(c.Name()) || row.Scope != c.definition.owner.Scope() || row.Single != (c.definition.policy.Cardinality == Single) || storage.DiskID(row.Disk) != c.definition.policy.Disk.ID() {
		return Attachment[M, K]{}, invalid()
	}
	if _, err := m.validateRow(row); err != nil {
		return Attachment[M, K]{}, err
	}
	identity, err := row.Identity.Decode()
	if err != nil {
		return Attachment[M, K]{}, err
	}
	owner, err := c.definition.owner.Parse(identity)
	if err != nil {
		return Attachment[M, K]{}, err
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		return Attachment[M, K]{}, err
	}
	digest, err := storage.ParseSHA256(row.Digest)
	if err != nil {
		return Attachment[M, K]{}, err
	}
	locale := value.Optional[i18n.LocaleID]{}
	if row.Locale != "" {
		locale = value.Set(i18n.LocaleID(row.Locale))
	}
	return Attachment[M, K]{id: model.IDFromBytes[FileOf[M]](row.ID.Bytes()), owner: owner, collection: Name(row.Collection), locale: locale, info: UploadInfo{OriginalName: row.OriginalName, MediaType: storage.MediaType(row.ContentType), Size: row.Size, Width: int(row.Width), Height: int(row.Height)}, disk: storage.DiskID(row.Disk), key: key, etag: storage.ETag(row.ETag), version: storage.VersionID(row.ObjectVersion), digest: digest, properties: row.Properties, position: row.SortOrder, created: row.CreatedAt}, nil
}
