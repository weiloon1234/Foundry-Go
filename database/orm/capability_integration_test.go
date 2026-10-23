package orm

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestCodecCapabilityPreservesMutatorRejection(t *testing.T) {
	if err := FoundryCodec(Codec(codec.String[string]()), sqlowner.Metadata()).Validate(); err != nil {
		t.Fatal("issued metadata capability rejected a codec", err)
	}
	calls := 0
	option := Mutate(func(v string) (string, error) {
		calls++
		return v, nil
	})
	if err := FoundryCodec(option, sqlowner.Metadata()).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("row codec bridge accepted a mutator option", err)
	}
	if calls != 0 {
		t.Fatal("codec bridge invoked a mutator")
	}
}
