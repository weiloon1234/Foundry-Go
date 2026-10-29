package attachmentstore

import (
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// FileTable binds the persisted File columns. Example: Files.Query().Limit(10).
type FileTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Files.Query()).
	orm.Table[File, model.ID[File]]
	// ID binds File.ID.
	// Example: orm.KeySet(Files.ID, id).
	ID orm.Key[File, model.ID[File]]
	// Owner binds File.Owner.
	// Example: orm.TextSet(Files.Owner, owner).
	Owner orm.Text[File, string]
	// Scope binds File.Scope.
	// Example: orm.TextSet(Files.Scope, scope).
	Scope orm.Text[File, string]
	// SubjectKey binds File.SubjectKey.
	// Example: orm.TextSet(Files.SubjectKey, subjectKey).
	SubjectKey orm.Text[File, string]
	// Identity binds File.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.JSONSet(Files.Identity, encoded) }.
	Identity orm.JSON[File, model.Identity]
	// Collection binds File.Collection.
	// Example: orm.TextSet(Files.Collection, collection).
	Collection orm.Text[File, string]
	// Locale binds File.Locale.
	// Example: orm.TextSet(Files.Locale, locale).
	Locale orm.Text[File, string]
	// Single binds File.Single.
	// Example: orm.ColumnSet(Files.Single, single).
	Single orm.Column[File, bool]
	// Disk binds File.Disk.
	// Example: orm.TextSet(Files.Disk, disk).
	Disk orm.Text[File, string]
	// ObjectKey binds File.ObjectKey.
	// Example: orm.TextSet(Files.ObjectKey, objectKey).
	ObjectKey orm.Text[File, string]
	// OriginalName binds File.OriginalName.
	// Example: orm.TextSet(Files.OriginalName, originalName).
	OriginalName orm.Text[File, string]
	// ContentType binds File.ContentType.
	// Example: orm.TextSet(Files.ContentType, contentType).
	ContentType orm.Text[File, string]
	// Size binds File.Size.
	// Example: orm.NumberSet(Files.Size, size).
	Size orm.Number[File, int64]
	// Digest binds File.Digest.
	// Example: orm.TextSet(Files.Digest, digest).
	Digest orm.Text[File, string]
	// ETag binds File.ETag.
	// Example: orm.TextSet(Files.ETag, etag).
	ETag orm.Text[File, string]
	// ObjectVersion binds File.ObjectVersion.
	// Example: orm.TextSet(Files.ObjectVersion, objectVersion).
	ObjectVersion orm.Text[File, string]
	// Width binds File.Width.
	// Example: orm.NumberSet(Files.Width, width).
	Width orm.Number[File, int32]
	// Height binds File.Height.
	// Example: orm.NumberSet(Files.Height, height).
	Height orm.Number[File, int32]
	// Properties binds File.Properties.
	// Example: encoded, err := value.NewJSON(properties); if err == nil { _ = orm.JSONSet(Files.Properties, encoded) }.
	Properties orm.JSON[File, json.RawMessage]
	// SortOrder binds File.SortOrder.
	// Example: orm.NumberSet(Files.SortOrder, sortOrder).
	SortOrder orm.Number[File, int32]
	// State binds File.State.
	// Example: orm.TextSet(Files.State, state).
	State orm.Text[File, string]
	// WriterID binds File.WriterID.
	// Example: orm.ColumnSet(Files.WriterID, writerID).
	WriterID orm.Column[File, model.ID[Writer]]
	// Attempts binds File.Attempts.
	// Example: orm.NumberSet(Files.Attempts, attempts).
	Attempts orm.Number[File, uint32]
	// LastFailure binds File.LastFailure.
	// Example: orm.TextSet(Files.LastFailure, lastFailure).
	LastFailure orm.Text[File, string]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.TimeSet(Files.CreatedAt, createdAt).
	CreatedAt orm.CreatedAt[File, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.TimeSet(Files.UpdatedAt, updatedAt).
	UpdatedAt orm.UpdatedAt[File, temporal.DateTime]
}

// Files owns the immutable File declaration. Example: orm.First(ctx, tx, Files.Query()).
var Files = orm.Define("foundry_attachments", func(b orm.Builder[File], m *File) FileTable {
	table := orm.TableOf(b, &m.ID)
	return FileTable{Table: table, ID: table.Key(), Owner: orm.TextOf(b, &m.Owner), Scope: orm.TextOf(b, &m.Scope), SubjectKey: orm.TextOf(b, &m.SubjectKey), Identity: orm.JSONOf(b, &m.Identity), Collection: orm.TextOf(b, &m.Collection), Locale: orm.TextOf(b, &m.Locale), Single: orm.ColumnOf(b, &m.Single), Disk: orm.TextOf(b, &m.Disk), ObjectKey: orm.TextOf(b, &m.ObjectKey), OriginalName: orm.TextOf(b, &m.OriginalName), ContentType: orm.TextOf(b, &m.ContentType), Size: orm.NumberOf(b, &m.Size), Digest: orm.TextOf(b, &m.Digest), ETag: orm.TextOf(b, &m.ETag), ObjectVersion: orm.TextOf(b, &m.ObjectVersion), Width: orm.NumberOf(b, &m.Width), Height: orm.NumberOf(b, &m.Height), Properties: orm.JSONOf(b, &m.Properties), SortOrder: orm.NumberOf(b, &m.SortOrder), State: orm.TextOf(b, &m.State), WriterID: orm.ColumnOf(b, &m.WriterID), Attempts: orm.NumberOf(b, &m.Attempts), LastFailure: orm.TextOf(b, &m.LastFailure), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

// VariantTable binds the persisted Variant columns. Example: Variants.Query().Limit(10).
type VariantTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Variants.Query()).
	orm.Table[Variant, model.ID[Variant]]
	// ID binds Variant.ID.
	// Example: orm.KeySet(Variants.ID, id).
	ID orm.Key[Variant, model.ID[Variant]]
	// FileID binds Variant.FileID.
	// Example: orm.ColumnSet(Variants.FileID, fileID).
	FileID orm.Column[Variant, model.ID[File]]
	// Name binds Variant.Name.
	// Example: orm.TextSet(Variants.Name, name).
	Name orm.Text[Variant, string]
	// Disk binds Variant.Disk.
	// Example: orm.TextSet(Variants.Disk, disk).
	Disk orm.Text[Variant, string]
	// ObjectKey binds Variant.ObjectKey.
	// Example: orm.TextSet(Variants.ObjectKey, objectKey).
	ObjectKey orm.Text[Variant, string]
	// ContentType binds Variant.ContentType.
	// Example: orm.TextSet(Variants.ContentType, contentType).
	ContentType orm.Text[Variant, string]
	// Size binds Variant.Size.
	// Example: orm.NumberSet(Variants.Size, size).
	Size orm.Number[Variant, int64]
	// Digest binds Variant.Digest.
	// Example: orm.TextSet(Variants.Digest, digest).
	Digest orm.Text[Variant, string]
	// ETag binds Variant.ETag.
	// Example: orm.TextSet(Variants.ETag, etag).
	ETag orm.Text[Variant, string]
	// ObjectVersion binds Variant.ObjectVersion.
	// Example: orm.TextSet(Variants.ObjectVersion, objectVersion).
	ObjectVersion orm.Text[Variant, string]
	// Width binds Variant.Width.
	// Example: orm.NumberSet(Variants.Width, width).
	Width orm.Number[Variant, int32]
	// Height binds Variant.Height.
	// Example: orm.NumberSet(Variants.Height, height).
	Height orm.Number[Variant, int32]
	// State binds Variant.State.
	// Example: orm.TextSet(Variants.State, state).
	State orm.Text[Variant, string]
	// WriterID binds Variant.WriterID.
	// Example: orm.ColumnSet(Variants.WriterID, writerID).
	WriterID orm.Column[Variant, model.ID[Writer]]
	// Attempts binds Variant.Attempts.
	// Example: orm.NumberSet(Variants.Attempts, attempts).
	Attempts orm.Number[Variant, uint32]
	// LastFailure binds Variant.LastFailure.
	// Example: orm.TextSet(Variants.LastFailure, lastFailure).
	LastFailure orm.Text[Variant, string]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.TimeSet(Variants.CreatedAt, createdAt).
	CreatedAt orm.CreatedAt[Variant, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.TimeSet(Variants.UpdatedAt, updatedAt).
	UpdatedAt orm.UpdatedAt[Variant, temporal.DateTime]
}

// Variants owns the immutable Variant declaration. Example: orm.First(ctx, tx, Variants.Query()).
var Variants = orm.Define("foundry_attachment_variants", func(b orm.Builder[Variant], m *Variant) VariantTable {
	table := orm.TableOf(b, &m.ID)
	return VariantTable{Table: table, ID: table.Key(), FileID: orm.ColumnOf(b, &m.FileID), Name: orm.TextOf(b, &m.Name), Disk: orm.TextOf(b, &m.Disk), ObjectKey: orm.TextOf(b, &m.ObjectKey), ContentType: orm.TextOf(b, &m.ContentType), Size: orm.NumberOf(b, &m.Size), Digest: orm.TextOf(b, &m.Digest), ETag: orm.TextOf(b, &m.ETag), ObjectVersion: orm.TextOf(b, &m.ObjectVersion), Width: orm.NumberOf(b, &m.Width), Height: orm.NumberOf(b, &m.Height), State: orm.TextOf(b, &m.State), WriterID: orm.ColumnOf(b, &m.WriterID), Attempts: orm.NumberOf(b, &m.Attempts), LastFailure: orm.TextOf(b, &m.LastFailure), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})
