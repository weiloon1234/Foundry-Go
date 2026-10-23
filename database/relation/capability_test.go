package relation

import (
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"testing"
)

func TestOwnedCollectionsRejectZeroCapability(t *testing.T) {
	var zero sqlowner.CollectionSeal
	if FoundryCollection(zero, []int{1}).IsLoaded() {
		t.Fatal("zero seal adopted a slice")
	}
	if FoundryLinked(zero, []Link[int, int]{{Model: 1, Pivot: 2}}).IsLoaded() {
		t.Fatal("zero seal adopted links")
	}
	if !FoundryCollection(sqlowner.Collections(), []int{1}).IsLoaded() {
		t.Fatal("issued seal rejected")
	}
}
