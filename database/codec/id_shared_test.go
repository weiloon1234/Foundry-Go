package codec

import (
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestSharedIDRequiresExactOwnerAndSeal(t *testing.T) {
	type owner struct{ Owner string }
	type other struct{ Other string }
	type integerKey int64
	type textKey string
	for _, err := range []error{
		FoundryID[owner, model.ID[other]](sqlowner.Metadata()).Validate(),
		FoundryID[owner, integerKey](sqlowner.Metadata()).Validate(),
		FoundryID[owner, textKey](sqlowner.Metadata()).Validate(),
		FoundryID[owner, model.ID[owner]](sqlowner.MetadataSeal{}).Validate(),
	} {
		if err == nil {
			t.Fatal("shared UUID codec accepted an invalid owner, representation or seal")
		}
	}
	if err := FoundryID[owner, model.ID[owner]](sqlowner.Metadata()).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedIDRoundTripAndAtomicDecode(t *testing.T) {
	type owner struct{ Name string }
	id, err := model.ParseID[owner]("01935c6d-85de-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []Codec[model.ID[owner]]{ID[owner](), FoundryID[owner, model.ID[owner]](sqlowner.Metadata())} {
		for _, want := range []model.ID[owner]{{}, id} {
			bound, err := c.Bind(want)
			if err != nil || bound != want.String() {
				t.Fatal("UUID bind changed", err)
			}
			for _, raw := range []any{bound, []byte(want.String())} {
				got, err := c.Decode(raw)
				if err != nil || got != want {
					t.Fatal("UUID round trip changed", err)
				}
			}
		}
		for _, raw := range []any{nil, "", "malformed", "01935c6d-85de-7000-8000-0000000000zz", []byte("01935c6d-85de-7000-8000-0000000000zz"), []byte{0xff}, int64(1), true} {
			destination := id
			if err := c.Scan(&destination).Scan(raw); err == nil || destination != id {
				t.Fatal("failed UUID scan published a value")
			}
			if got, err := c.Decode(raw); err == nil || got != (model.ID[owner]{}) {
				t.Fatal("failed UUID decode returned a partial value")
			}
			if err := decodeSharedID(raw, unsafe.Pointer(&destination)); err == nil || destination != id {
				t.Fatal("shared UUID parser changed its destination on failure")
			}
		}
	}
}
