package mutators

import (
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:model table=input_key_records
type KeyRecord struct {
	// Foundry field behavior (generated): KeyRecord.ID retains stored input_key_records.id. Custom setter: [KeyRecord.MutateID] transforms assigned values during persistence through [KeyRecordDraft.SetID]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts mutators.FreshID and produces stored model.ID[mutators.KeyRecord]; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	ID    model.ID[KeyRecord]
	Label string `foundry:"default=database"`
}
type FreshID struct{ Value model.ID[KeyRecord] }

func (KeyRecord) MutateID(input FreshID) (model.ID[KeyRecord], error) { return input.Value, nil }
