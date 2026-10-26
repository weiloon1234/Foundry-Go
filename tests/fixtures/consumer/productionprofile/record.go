// Package productionprofile is a small measurement fixture, not a starter app.
package productionprofile

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Record struct {
	ID      model.ID[Record]
	Name    string
	Active  bool
	Score   int64
	Payload []byte
	Note    value.Nullable[string]
	// Measurement harness inserts one field here in its independent copy.
}
