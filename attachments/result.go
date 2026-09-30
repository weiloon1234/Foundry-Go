package attachments

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type FileOf[M any] struct{ _ [0]*M }
type ID[M any] = model.ID[FileOf[M]]

// OperationID is the administrative/job reference for one durable upload intent.
// Application collection operations use model-owned ID[M] instead.
type Operation struct{}
type OperationID = model.ID[Operation]
type Publication uint8

const (
	Unpublished Publication = iota
	Published
	PublicationUnknown
)

// Result distinguishes ownership publication from storage cleanup. A published
// attachment remains published when old-object cleanup returns an error. An
// uncertain publication is never described as rolled back or automatically
// retried. Inspect the durable Operation before deciding the next action.
// PendingVariants reports that declared variants were queued, or that their
// synchronous generation failed; the original is published either way.
type Result[M any, K comparable] struct {
	Operation       OperationID
	Publication     Publication
	Attachment      value.Optional[Attachment[M, K]]
	PendingCleanup  []OperationID
	PendingVariants bool
}

func (Result[M, K]) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("attachment upload result")) }
func (Result[M, K]) MarshalJSON() ([]byte, error) { return nil, invalid() }

type UploadInfo struct {
	OriginalName  string
	MediaType     storage.MediaType
	Size          int64
	Width, Height int
}

// File is the owner-free view of one ready attachment: identity, collection,
// detected metadata, properties and ready variants. Model slots hold Files;
// Attachment adds the typed owner reference.
type File[M any] struct {
	id         ID[M]
	collection Name
	locale     value.Optional[i18n.LocaleID]
	info       UploadInfo
	disk       storage.DiskID
	key        storage.ObjectKey
	etag       storage.ETag
	version    storage.VersionID
	digest     storage.SHA256
	properties value.JSON[json.RawMessage]
	position   int32
	created    temporal.DateTime
	variants   []storedVariant
}

func (f File[M]) ID() ID[M]                             { return f.id }
func (f File[M]) Collection() Name                      { return f.collection }
func (f File[M]) Locale() value.Optional[i18n.LocaleID] { return f.locale }
func (f File[M]) Info() UploadInfo                      { return f.info }
func (f File[M]) Position() int32                       { return f.position }
func (f File[M]) CreatedAt() temporal.DateTime          { return f.created }
func (f File[M]) Properties() (json.RawMessage, error)  { return f.properties.Decode() }
func (f File[M]) IsZero() bool                          { return f.id.IsZero() }
func (File[M]) Format(s fmt.State, _ rune)              { _, _ = s.Write([]byte("model attachment file")) }
func (File[M]) MarshalJSON() ([]byte, error)            { return nil, invalid() }

// Attachment is a ready File with its typed owner reference.
type Attachment[M any, K comparable] struct {
	File[M]
	owner model.Reference[M, K]
}

func (a Attachment[M, K]) Owner() model.Reference[M, K] { return a.owner }
func (Attachment[M, K]) Format(s fmt.State, _ rune)     { _, _ = s.Write([]byte("model attachment")) }
func (Attachment[M, K]) MarshalJSON() ([]byte, error)   { return nil, invalid() }
