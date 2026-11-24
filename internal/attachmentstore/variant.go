package attachmentstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Variant is one derived file of a ready attachment, for example a thumbnail.
// Its object is written under its own intent and never replaces the original.
//
//foundry:model table=foundry_attachment_variants var=Variants key=ID identity=false
type Variant struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Variants.Query()).
	//
	// ID binds Variant.ID.
	// Example: orm.Set(Variants.ID, id).
	ID model.ID[Variant] `foundry:"member=ID"`
	// FileID binds Variant.FileID.
	// Example: orm.Set(Variants.FileID, fileID).
	FileID model.ID[File] `foundry:"role=column"`
	// Name binds Variant.Name.
	// Example: orm.Set(Variants.Name, name).
	Name string
	// Disk binds Variant.Disk.
	// Example: orm.Set(Variants.Disk, disk).
	Disk string
	// ObjectKey binds Variant.ObjectKey.
	// Example: orm.Set(Variants.ObjectKey, objectKey).
	ObjectKey string
	// ContentType binds Variant.ContentType.
	// Example: orm.Set(Variants.ContentType, contentType).
	ContentType string
	// Size binds Variant.Size.
	// Example: orm.Set(Variants.Size, size).
	Size int64
	// Digest binds Variant.Digest.
	// Example: orm.Set(Variants.Digest, digest).
	Digest string
	// ETag binds Variant.ETag.
	// Example: orm.Set(Variants.ETag, etag).
	ETag string `foundry:"column=etag"`
	// ObjectVersion binds Variant.ObjectVersion.
	// Example: orm.Set(Variants.ObjectVersion, objectVersion).
	ObjectVersion string
	// Width binds Variant.Width.
	// Example: orm.Set(Variants.Width, width).
	Width int32
	// Height binds Variant.Height.
	// Example: orm.Set(Variants.Height, height).
	Height int32
	// State binds Variant.State.
	// Example: orm.Set(Variants.State, state).
	State string
	// WriterID binds Variant.WriterID.
	// Example: orm.Set(Variants.WriterID, writerID).
	WriterID model.ID[Writer] `foundry:"role=column"`
	// Attempts binds Variant.Attempts.
	// Example: orm.Set(Variants.Attempts, attempts).
	Attempts uint32
	// LastFailure binds Variant.LastFailure.
	// Example: orm.Set(Variants.LastFailure, lastFailure).
	LastFailure string
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	//
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Variants.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	//
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Variants.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
}

func (Variant) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("attachment variant")) }
