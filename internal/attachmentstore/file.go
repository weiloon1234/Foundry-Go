// Package attachmentstore owns ordinary generated attachment intent and file
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

//foundry:model table=foundry_attachments primary=ID
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
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func (File) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("attachment intent")) }

// Inspection does not materialize custom properties or upload content metadata.
//
//foundry:projection
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
