package attachmentstore

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// FileTable binds the persisted File columns. Example: Files.Query().Limit(10).
type FileTable struct {
	// Table provides model queries and writes. Example: Files.Query().First(ctx,tx).
	orm.Table[File, model.ID[File]]
	// ID binds File.ID.
	// Example: Files.ID.Set(id).
	ID orm.Key[File, model.ID[File]]
	// Owner binds File.Owner.
	// Example: Files.Owner.Set(owner).
	Owner orm.Text[File, string]
	// Scope binds File.Scope.
	// Example: Files.Scope.Set(scope).
	Scope orm.Text[File, string]
	// SubjectKey binds File.SubjectKey.
	// Example: Files.SubjectKey.Set(subjectKey).
	SubjectKey orm.Text[File, string]
	// Identity binds File.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = Files.Identity.Set(encoded) }.
	Identity orm.JSON[File, model.Identity]
	// Collection binds File.Collection.
	// Example: Files.Collection.Set(collection).
	Collection orm.Text[File, string]
	// Locale binds File.Locale.
	// Example: Files.Locale.Set(locale).
	Locale orm.Text[File, string]
	// Single binds File.Single.
	// Example: Files.Single.Set(single).
	Single orm.Column[File, bool]
	// Disk binds File.Disk.
	// Example: Files.Disk.Set(disk).
	Disk orm.Text[File, string]
	// ObjectKey binds File.ObjectKey.
	// Example: Files.ObjectKey.Set(objectKey).
	ObjectKey orm.Text[File, string]
	// OriginalName binds File.OriginalName.
	// Example: Files.OriginalName.Set(originalName).
	OriginalName orm.Text[File, string]
	// ContentType binds File.ContentType.
	// Example: Files.ContentType.Set(contentType).
	ContentType orm.Text[File, string]
	// Size binds File.Size.
	// Example: Files.Size.Set(size).
	Size orm.Number[File, int64]
	// Digest binds File.Digest.
	// Example: Files.Digest.Set(digest).
	Digest orm.Text[File, string]
	// ETag binds File.ETag.
	// Example: Files.ETag.Set(etag).
	ETag orm.Text[File, string]
	// ObjectVersion binds File.ObjectVersion.
	// Example: Files.ObjectVersion.Set(objectVersion).
	ObjectVersion orm.Text[File, string]
	// Width binds File.Width.
	// Example: Files.Width.Set(width).
	Width orm.Number[File, int32]
	// Height binds File.Height.
	// Example: Files.Height.Set(height).
	Height orm.Number[File, int32]
	// Properties binds File.Properties.
	// Example: encoded, err := value.NewJSON(properties); if err == nil { _ = Files.Properties.Set(encoded) }.
	Properties orm.JSON[File, json.RawMessage]
	// SortOrder binds File.SortOrder.
	// Example: Files.SortOrder.Set(sortOrder).
	SortOrder orm.Number[File, int32]
	// State binds File.State.
	// Example: Files.State.Set(state).
	State orm.Text[File, string]
	// WriterID binds File.WriterID.
	// Example: Files.WriterID.Set(writerID).
	WriterID orm.Column[File, model.ID[Writer]]
	// Attempts binds File.Attempts.
	// Example: Files.Attempts.Set(attempts).
	Attempts orm.Number[File, uint32]
	// LastFailure binds File.LastFailure.
	// Example: Files.LastFailure.Set(lastFailure).
	LastFailure orm.Text[File, string]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: Files.CreatedAt.Set(createdAt).
	CreatedAt orm.CreatedAt[File, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: Files.UpdatedAt.Set(updatedAt).
	UpdatedAt orm.UpdatedAt[File, temporal.DateTime]
}

// Files owns the immutable File declaration. Example: Files.Query().First(ctx,tx).
var Files = orm.Define("foundry_attachments", func(b orm.Builder[File], m *File) FileTable {
	table := orm.TableOf(b, &m.ID)
	return FileTable{Table: table, ID: table.Key(), Owner: orm.TextOf(b, &m.Owner), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Identity: orm.JSONOf(b, &m.Identity), Collection: orm.TextOf(b, &m.Collection), Locale: orm.TextOf(b, &m.Locale), Single: orm.ColumnOf(b, &m.Single), Disk: orm.TextOf(b, &m.Disk), ObjectKey: orm.TextOf(b, &m.ObjectKey), OriginalName: orm.TextOf(b, &m.OriginalName), ContentType: orm.TextOf(b, &m.ContentType), Size: orm.NumberOf(b, &m.Size), Digest: orm.TextOf(b, &m.Digest), ETag: orm.TextOf(b, &m.ETag), ObjectVersion: orm.TextOf(b, &m.ObjectVersion), Width: orm.NumberOf(b, &m.Width), Height: orm.NumberOf(b, &m.Height), Properties: orm.JSONOf(b, &m.Properties), SortOrder: orm.NumberOf(b, &m.SortOrder), State: orm.TextOf(b, &m.State), WriterID: orm.ColumnOf(b, &m.WriterID), Attempts: orm.NumberOf(b, &m.Attempts), LastFailure: orm.TextOf(b, &m.LastFailure), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

// VariantTable binds the persisted Variant columns. Example: Variants.Query().Limit(10).
type VariantTable struct {
	// Table provides model queries and writes. Example: Variants.Query().First(ctx,tx).
	orm.Table[Variant, model.ID[Variant]]
	// ID binds Variant.ID.
	// Example: Variants.ID.Set(id).
	ID orm.Key[Variant, model.ID[Variant]]
	// FileID binds Variant.FileID.
	// Example: Variants.FileID.Set(fileID).
	FileID orm.Column[Variant, model.ID[File]]
	// Name binds Variant.Name.
	// Example: Variants.Name.Set(name).
	Name orm.Text[Variant, string]
	// Disk binds Variant.Disk.
	// Example: Variants.Disk.Set(disk).
	Disk orm.Text[Variant, string]
	// ObjectKey binds Variant.ObjectKey.
	// Example: Variants.ObjectKey.Set(objectKey).
	ObjectKey orm.Text[Variant, string]
	// ContentType binds Variant.ContentType.
	// Example: Variants.ContentType.Set(contentType).
	ContentType orm.Text[Variant, string]
	// Size binds Variant.Size.
	// Example: Variants.Size.Set(size).
	Size orm.Number[Variant, int64]
	// Digest binds Variant.Digest.
	// Example: Variants.Digest.Set(digest).
	Digest orm.Text[Variant, string]
	// ETag binds Variant.ETag.
	// Example: Variants.ETag.Set(etag).
	ETag orm.Text[Variant, string]
	// ObjectVersion binds Variant.ObjectVersion.
	// Example: Variants.ObjectVersion.Set(objectVersion).
	ObjectVersion orm.Text[Variant, string]
	// Width binds Variant.Width.
	// Example: Variants.Width.Set(width).
	Width orm.Number[Variant, int32]
	// Height binds Variant.Height.
	// Example: Variants.Height.Set(height).
	Height orm.Number[Variant, int32]
	// State binds Variant.State.
	// Example: Variants.State.Set(state).
	State orm.Text[Variant, string]
	// WriterID binds Variant.WriterID.
	// Example: Variants.WriterID.Set(writerID).
	WriterID orm.Column[Variant, model.ID[Writer]]
	// Attempts binds Variant.Attempts.
	// Example: Variants.Attempts.Set(attempts).
	Attempts orm.Number[Variant, uint32]
	// LastFailure binds Variant.LastFailure.
	// Example: Variants.LastFailure.Set(lastFailure).
	LastFailure orm.Text[Variant, string]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: Variants.CreatedAt.Set(createdAt).
	CreatedAt orm.CreatedAt[Variant, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: Variants.UpdatedAt.Set(updatedAt).
	UpdatedAt orm.UpdatedAt[Variant, temporal.DateTime]
}

// Variants owns the immutable Variant declaration. Example: Variants.Query().First(ctx,tx).
var Variants = orm.Define("foundry_attachment_variants", func(b orm.Builder[Variant], m *Variant) VariantTable {
	table := orm.TableOf(b, &m.ID)
	return VariantTable{Table: table, ID: table.Key(), FileID: orm.ColumnOf(b, &m.FileID), Name: orm.TextOf(b, &m.Name), Disk: orm.TextOf(b, &m.Disk), ObjectKey: orm.TextOf(b, &m.ObjectKey), ContentType: orm.TextOf(b, &m.ContentType), Size: orm.NumberOf(b, &m.Size), Digest: orm.TextOf(b, &m.Digest), ETag: orm.TextOf(b, &m.ETag), ObjectVersion: orm.TextOf(b, &m.ObjectVersion), Width: orm.NumberOf(b, &m.Width), Height: orm.NumberOf(b, &m.Height), State: orm.TextOf(b, &m.State), WriterID: orm.ColumnOf(b, &m.WriterID), Attempts: orm.NumberOf(b, &m.Attempts), LastFailure: orm.TextOf(b, &m.LastFailure), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})
