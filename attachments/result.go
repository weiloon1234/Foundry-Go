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
type Result[M any, K comparable] struct {
	Operation      OperationID
	Publication    Publication
	Attachment     value.Optional[Attachment[M, K]]
	PendingCleanup []OperationID
}

func (Result[M, K]) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("attachment upload result")) }
func (Result[M, K]) MarshalJSON() ([]byte, error) { return nil, invalid() }

type UploadInfo struct {
	OriginalName  string
	MediaType     storage.MediaType
	Size          int64
	Width, Height int
}
type Attachment[M any, K comparable] struct {
	id         ID[M]
	owner      model.Reference[M, K]
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
}

func (a Attachment[M, K]) ID() ID[M]                             { return a.id }
func (a Attachment[M, K]) Owner() model.Reference[M, K]          { return a.owner }
func (a Attachment[M, K]) Collection() Name                      { return a.collection }
func (a Attachment[M, K]) Locale() value.Optional[i18n.LocaleID] { return a.locale }
func (a Attachment[M, K]) Info() UploadInfo                      { return a.info }
func (a Attachment[M, K]) Position() int32                       { return a.position }
func (a Attachment[M, K]) CreatedAt() temporal.DateTime          { return a.created }
func (a Attachment[M, K]) Properties() (json.RawMessage, error)  { return a.properties.Decode() }
func (a Attachment[M, K]) IsZero() bool                          { return a.id.IsZero() }
func (Attachment[M, K]) Format(s fmt.State, _ rune)              { _, _ = s.Write([]byte("model attachment")) }
func (Attachment[M, K]) MarshalJSON() ([]byte, error)            { return nil, invalid() }
