// Package attachmentstore owns ordinary declared attachment intent and file
// records. Public attachment APIs own state transitions and storage effects.
package attachmentstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Writer struct{}

//foundry:model table=foundry_attachments var=Files key=ID identity=false
type File struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Files.Query()).
	//
	// ID binds File.ID.
	// Example: orm.Set(Files.ID, id).
	ID model.ID[File] `foundry:"member=ID"`
	// Owner binds File.Owner.
	// Example: orm.Set(Files.Owner, owner).
	Owner string
	// Scope binds File.Scope.
	// Example: orm.Set(Files.Scope, scope).
	Scope string
	// SubjectKey binds File.SubjectKey.
	// Example: orm.Set(Files.SubjectKey, subjectKey).
	SubjectKey string
	// Identity binds File.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.Set(Files.Identity, encoded) }.
	Identity value.JSON[model.Identity]
	// Collection binds File.Collection.
	// Example: orm.Set(Files.Collection, collection).
	Collection string
	// Locale binds File.Locale.
	// Example: orm.Set(Files.Locale, locale).
	Locale string
	// Single binds File.Single.
	// Example: orm.Set(Files.Single, single).
	Single bool `foundry:"role=column"`
	// Disk binds File.Disk.
	// Example: orm.Set(Files.Disk, disk).
	Disk string
	// ObjectKey binds File.ObjectKey.
	// Example: orm.Set(Files.ObjectKey, objectKey).
	ObjectKey string
	// OriginalName binds File.OriginalName.
	// Example: orm.Set(Files.OriginalName, originalName).
	OriginalName string
	// ContentType binds File.ContentType.
	// Example: orm.Set(Files.ContentType, contentType).
	ContentType string
	// Size binds File.Size.
	// Example: orm.Set(Files.Size, size).
	Size int64
	// Digest binds File.Digest.
	// Example: orm.Set(Files.Digest, digest).
	Digest string
	// ETag binds File.ETag.
	// Example: orm.Set(Files.ETag, etag).
	ETag string `foundry:"column=etag"`
	// ObjectVersion binds File.ObjectVersion.
	// Example: orm.Set(Files.ObjectVersion, objectVersion).
	ObjectVersion string
	// Width binds File.Width.
	// Example: orm.Set(Files.Width, width).
	Width int32
	// Height binds File.Height.
	// Example: orm.Set(Files.Height, height).
	Height int32
	// Properties binds File.Properties.
	// Example: encoded, err := value.NewJSON(properties); if err == nil { _ = orm.Set(Files.Properties, encoded) }.
	Properties value.JSON[json.RawMessage]
	// SortOrder binds File.SortOrder.
	// Example: orm.Set(Files.SortOrder, sortOrder).
	SortOrder int32
	// State binds File.State.
	// Example: orm.Set(Files.State, state).
	State string
	// WriterID binds File.WriterID.
	// Example: orm.Set(Files.WriterID, writerID).
	WriterID model.ID[Writer] `foundry:"role=column"`
	// Attempts binds File.Attempts.
	// Example: orm.Set(Files.Attempts, attempts).
	Attempts uint32
	// LastFailure binds File.LastFailure.
	// Example: orm.Set(Files.LastFailure, lastFailure).
	LastFailure string
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	//
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Files.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	//
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Files.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
}

func (File) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("attachment intent")) }

// Inspection does not materialize custom properties or upload content metadata.
type FileIndex struct {
	ID         model.ID[File]
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Collection string
	Locale     string
	State      string
	Disk       string
	ObjectKey  string
	UpdatedAt  temporal.DateTime
}
