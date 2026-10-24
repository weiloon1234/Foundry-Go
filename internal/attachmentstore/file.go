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

type File struct {
	ID            model.ID[File]
	Owner         string
	Scope         string
	SubjectKey    string
	Identity      value.JSON[model.Identity]
	Collection    string
	Locale        string
	Single        bool
	Disk          string
	ObjectKey     string
	OriginalName  string
	ContentType   string
	Size          int64
	Digest        string
	ETag          string `foundry:"column=etag"`
	ObjectVersion string
	Width         int32
	Height        int32
	Properties    value.JSON[json.RawMessage]
	SortOrder     int32
	State         string
	WriterID      model.ID[Writer]
	Attempts      uint32
	LastFailure   string
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	CreatedAt temporal.DateTime
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	UpdatedAt temporal.DateTime
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
