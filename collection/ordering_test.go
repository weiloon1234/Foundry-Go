package collection_test

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/collection"
)

type product struct {
	Name     string
	Category string
	Price    int
}

type products []product

func TestChunkOwnsCappedChunks(t *testing.T) {
	input := []int{1, 2, 3, 4, 5}
	chunks := collection.Chunk(input, 2)
	if !reflect.DeepEqual(chunks, [][]int{{1, 2}, {3, 4}, {5}}) {
		t.Fatal(chunks)
	}
	chunks[0] = append(chunks[0], 99)
	chunks[1][0] = 42
	if chunks[1][0] != 42 || chunks[1][1] != 4 || !slices.Equal(input, []int{1, 2, 3, 4, 5}) {
		t.Fatal("chunk append overwrote a neighbor or the input", chunks, input)
	}
	if collection.Chunk([]int(nil), 3) != nil || len(collection.Chunk([]int{}, 3)) != 0 {
		t.Fatal("empty chunk semantics")
	}
	if allocations := testing.AllocsPerRun(20, func() { _ = collection.Chunk(input, 2) }); allocations != 2 {
		t.Fatal("chunking should allocate one backing array and one outer slice", allocations)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("non-positive chunk size accepted")
		}
	}()
	collection.Chunk(input, 0)
}

func TestUniqueSortAndSelection(t *testing.T) {
	if unique := collection.Unique([]string{"b", "a", "b", "c", "a"}); !slices.Equal(unique, []string{"b", "a", "c"}) {
		t.Fatal(unique)
	}
	input := products{{"pen", "office", 3}, {"desk", "furniture", 120}, {"clip", "office", 1}, {"lamp", "furniture", 120}}
	calls := 0
	sorted := collection.SortBy(input, func(p product) int { calls++; return p.Price })
	if calls != len(input) || collection.Map(sorted, func(p product) string { return p.Name })[0] != "clip" || sorted[2].Name != "desk" || sorted[3].Name != "lamp" {
		t.Fatal("stable ascending order or single key evaluation failed", sorted, calls)
	}
	descending := collection.SortByDesc(input, func(p product) int { return p.Price })
	if names := collection.Map(descending, func(p product) string { return p.Name }); !slices.Equal(names, []string{"desk", "lamp", "pen", "clip"}) {
		t.Fatal(names)
	}
	sorted[0].Name = "changed"
	if input[2].Name != "clip" {
		t.Fatal("sorted result reused the input container")
	}
	floats := collection.SortBy([]float64{2, math.NaN(), 1}, func(v float64) float64 { return v })
	if !math.IsNaN(floats[0]) || floats[1] != 1 {
		t.Fatal(floats)
	}
	cheapest, ok := collection.MinBy(input, func(p product) int { return p.Price })
	priciest, _ := collection.MaxBy(input, func(p product) int { return p.Price })
	if !ok || cheapest.Name != "clip" || priciest.Name != "desk" {
		t.Fatal(cheapest, priciest)
	}
	if _, ok := collection.MinBy(products(nil), func(p product) int { return p.Price }); ok {
		t.Fatal("empty minimum")
	}
}

func TestNumericAggregates(t *testing.T) {
	input := products{{"pen", "office", 3}, {"desk", "furniture", 120}, {"clip", "office", 1}}
	if collection.Sum([]int{1, 2, 3}) != 6 || collection.SumBy(input, func(p product) int { return p.Price }) != 124 {
		t.Fatal("sum")
	}
	if average, ok := collection.Average([]int{1, 2}); !ok || average != 1.5 {
		t.Fatal(average, ok)
	}
	if average, ok := collection.AverageBy(input, func(p product) float64 { return float64(p.Price) }); !ok || math.Abs(average-124.0/3) > 1e-12 {
		t.Fatal(average, ok)
	}
	if _, ok := collection.Average([]int(nil)); ok {
		t.Fatal("empty average")
	}
	counts := collection.CountBy(input, func(p product) string { return p.Category })
	if !reflect.DeepEqual(counts, map[string]int{"office": 2, "furniture": 1}) {
		t.Fatal(counts)
	}
}
