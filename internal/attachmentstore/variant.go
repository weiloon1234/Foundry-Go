package attachmentstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Variant is one derived file of a ready attachment, for example a thumbnail.
// Its object is written under its own intent and never replaces the original.
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
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	CreatedAt temporal.DateTime
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	UpdatedAt temporal.DateTime
}

func (Variant) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("attachment variant")) }
