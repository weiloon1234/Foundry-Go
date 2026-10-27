package relation_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func zeroCollection[K, M, R any](f func(K, []M) R, items []M) R {
	var k K
	return f(k, items)
}

func TestFrameworkCollectionsValidateAndCopyInputs(t *testing.T) {
	items := []int{1, 2}
	if zeroCollection(relation.FoundryCollection[int], items).IsLoaded() {
		t.Fatal("zero seal published collection")
	}
	many := relation.FoundryCollection(sqlowner.Collections(), items)
	items[0] = 99
	copied, loaded := many.Get()
	if !loaded || copied[0] != 1 {
		t.Fatal("collection aliased input", copied)
	}
	copied[0] = 98
	for i, item := range many.All() {
		if item != i+1 {
			t.Fatal("published collection changed", item)
		}
	}
	links := []relation.Link[int, int]{{Model: 1, Pivot: 2}}
	if zeroCollection(relation.FoundryLinked[int, int], links).IsLoaded() {
		t.Fatal("zero seal published links")
	}
	through := relation.FoundryLinked(sqlowner.Collections(), links)
	links[0] = relation.Link[int, int]{Model: 3, Pivot: 4}
	copiedLinks, loaded := through.Get()
	if !loaded || copiedLinks[0].Model != 1 || copiedLinks[0].Pivot != 2 {
		t.Fatal("links aliased input", copiedLinks)
	}
	copiedLinks[0].Pivot = 5
	for _, link := range through.All() {
		if link.Model != 1 || link.Pivot != 2 {
			t.Fatal("published links changed", link)
		}
	}
	if !relation.FoundryCollection[int](sqlowner.Collections(), nil).IsLoaded() || !relation.FoundryLinked[int, int](sqlowner.Collections(), nil).IsLoaded() {
		t.Fatal("valid empty collections lost loaded state")
	}
}
