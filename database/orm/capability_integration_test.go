package orm

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestCodecCapabilityAcceptsOnlyValidCodecOptions(t *testing.T) {
	if err := FoundryCodec(Codec(codec.String[string]()), sqlowner.Metadata()).Validate(); err != nil {
		t.Fatal("issued metadata capability rejected a codec", err)
	}
	for _, seal := range []sqlowner.MetadataSeal{sqlowner.Metadata(), {}} {
		if err := FoundryCodec(ColumnOption[string]{}, seal).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("empty codec option accepted", err)
		}
	}
	if err := FoundryCodec(Codec(codec.String[string]()), sqlowner.MetadataSeal{}).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unissued capability accepted", err)
	}
}
