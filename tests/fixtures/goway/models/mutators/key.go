package mutators

import (
	"github.com/weiloon1234/Foundry-Go/model"
)

// KeyRecord exercises a distinct primary-key input.
//
//foundry:model table=input_key_records var=KeyRecords key=ID identity=false
type KeyRecord struct {
	ID    model.ID[KeyRecord] `foundry:"input=FreshID,member=ID,key-role=input"`
	Label string              `foundry:"default=database"`
}
type FreshID struct{ Value model.ID[KeyRecord] }

func (KeyRecord) MutateID(input FreshID) (model.ID[KeyRecord], error) { return input.Value, nil }
