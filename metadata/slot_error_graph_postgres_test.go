package metadata

import (
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
)

var inspectionDecodes atomic.Int32

type InspectionValue struct{ Mode string }

func (v InspectionValue) MarshalJSON() ([]byte, error) { return json.Marshal(v.Mode) }
func (v *InspectionValue) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &v.Mode); err != nil {
		return err
	}
	// Validation succeeds; the hostile decoder fails when the slot materializes it.
	if inspectionDecodes.Add(1) == 1 {
		return nil
	}
	return &errorgraphtest.Error{Mode: v.Mode}
}
func TestPostgresMetadataSlotContainsDecoderErrorMatching(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	id := contract.TypeID("github.com/weiloon1234/Foundry-Go/metadata.InspectionValue")
	codec := contract.DefineJSONValue[InspectionValue](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
	assigned := 0
	slot := defineValue(extensiontest.Members, "hostile", Spec[InspectionValue]{JSON: codec}, contract.JSON[InspectionValue]{}, extensions.SlotBinding[extensiontest.Member, int64, Value[InspectionValue]]{
		Field: "Metadata", Reference: func(m extensiontest.Member) model.Reference[extensiontest.Member, int64] {
			return extensions.ORMIdentity(extensiontest.Members).Reference(m.ID)
		},
		Get: func(extensiontest.Member) Value[InspectionValue] { return Value[InspectionValue]{} },
		Set: func(m extensiontest.Member, _ Value[InspectionValue]) extensiontest.Member { assigned++; return m },
	})
	manager, err := New(f.Store, slot.registration())
	if err != nil {
		t.Fatal(err)
	}
	slot = slot.from(manager)
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		if err := slot.keyDeclaration().Set(t.Context(), manager, extensions.ORMIdentity(extensiontest.Members).Reference(1), InspectionValue{mode}); err != nil {
			t.Fatal(err)
		}
		inspectionDecodes.Store(0)
		// The native ORM delegates extension loading to this same slot owner.
		limits := querycore.DefaultRelationLimits()
		remaining, attached := limits.MaxRows, 0
		_, err := slot.loader.Load(t.Context(), f.DB, []extensiontest.Member{{ID: 1}}, limits, &remaining, &attached, false)
		var cause *errorgraphtest.Error
		if err == nil || !errors.As(err, &cause) || cause.Formatted.Load() != 0 || cause.Visits.Load() > 1024 || assigned != 0 || inspectionDecodes.Load() != 2 {
			t.Fatal("metadata decode error became missing, retried, or lost identity")
		}
	}
}
