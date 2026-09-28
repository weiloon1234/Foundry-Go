// Package productionprofile is a small measurement fixture, not a starter app.
package productionprofile

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=profile_records
type Record struct {
	ID     model.ID[Record]
	Name   string
	Active bool
	Score  int64
	// Foundry field behavior (generated): Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.
	Payload []byte
	Note    value.Nullable[string]
	// Measurement harness inserts one field here in its independent copy.
}
