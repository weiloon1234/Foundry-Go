package collection_test

import (
	"github.com/weiloon1234/Foundry-Go/collection"
	"reflect"
	"slices"
	"testing"
)

func TestTypedCollectionsPreserveOrderAndContainerOwnership(t *testing.T) {
	type row struct {
		Key  int
		Name string
	}
	type rows []row
	input := rows{{1, "first"}, {2, "second"}, {1, "last"}}
	key := func(v row) int { return v.Key }
	group := collection.GroupBy(input, key)
	if !reflect.DeepEqual(group[1], []row{{1, "first"}, {1, "last"}}) {
		t.Fatal(group)
	}
	keyed := collection.KeyBy(input, key)
	if keyed[1].Name != "last" {
		t.Fatal(keyed)
	}
	unique := collection.UniqueBy(input, key)
	if !reflect.DeepEqual(unique, input[:2]) {
		t.Fatal(unique)
	}
	mapped := collection.Map(input, func(v row) string { return v.Name })
	if !slices.Equal(mapped, []string{"first", "second", "last"}) {
		t.Fatal(mapped)
	}
	selected := collection.Filter(input, func(v row) bool { return v.Key == 1 })
	selected[0].Name = "changed"
	group[1][0].Name = "changed"
	unique[0].Name = "changed"
	if input[0].Name != "first" {
		t.Fatal("result reused source container")
	}
	yes, no := collection.Partition(input, func(v row) bool { return v.Key == 1 })
	if len(yes) != 2 || len(no) != 1 || yes[1].Name != "last" {
		t.Fatal(yes, no)
	}
	flat := collection.FlatMap(input, func(v row) []int { return []int{v.Key, v.Key} })
	if !slices.Equal(flat, []int{1, 1, 2, 2, 1, 1}) {
		t.Fatal(flat)
	}
	if total := collection.Reduce(input, 0, func(n int, v row) int { return n + v.Key }); total != 4 {
		t.Fatal(total)
	}
	if found, ok := collection.Find(input, func(v row) bool { return v.Key == 1 }); !ok || found.Name != "first" {
		t.Fatal(found, ok)
	}
	if collection.Count(input, func(v row) bool { return v.Key == 1 }) != 2 || !collection.Any(input, func(v row) bool { return v.Key == 2 }) || collection.All(input, func(v row) bool { return v.Key == 1 }) {
		t.Fatal("predicate helpers")
	}
	var empty rows
	if collection.Map(empty, key) != nil || collection.Filter(empty, func(row) bool { return true }) != nil || !collection.All(empty, func(row) bool { return false }) {
		t.Fatal("empty semantics")
	}
}
