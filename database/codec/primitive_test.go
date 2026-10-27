package codec_test

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"testing"
)

func TestPrimitiveCapability(t *testing.T) {
	if err := codec.FoundryPrimitive[int](sqlowner.MetadataSeal{}).Validate(); err == nil {
		t.Fatal("zero capability accepted")
	}
	if err := codec.FoundryPrimitive[struct{}](sqlowner.Metadata()).Validate(); err == nil {
		t.Fatal("unsupported representation accepted")
	}
	if err := codec.FoundryPrimitive[int](sqlowner.Metadata()).Validate(); err != nil {
		t.Fatal(err)
	}
}
