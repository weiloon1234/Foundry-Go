package attachmentstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Variant is one derived file of a ready attachment, for example a thumbnail.
// Its object is written under its own intent and never replaces the original.
//
//foundry:model table=foundry_attachment_variants primary=ID
type Variant struct {
	ID            model.ID[Variant]
	FileID        model.ID[File]
	Name          string
	Disk          string
	ObjectKey     string
	ContentType   string
	Size          int64
	Digest        string
	ETag          string `foundry:"column=etag"`
	ObjectVersion string
	Width         int32
	Height        int32
	State         string
	WriterID      model.ID[Writer]
	Attempts      uint32
	LastFailure   string
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func (Variant) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("attachment variant")) }
