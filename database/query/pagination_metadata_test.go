package query_test

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"math"
	"testing"
)

func TestPageMetadataValidationPreservesSnapshotSemantics(t *testing.T) {
	t.Parallel()
	for _, request := range []query.PageRequest{{}, {Number: 1, Size: 0}, {Number: 0, Size: 1}, {Number: 1, Size: query.MaxPageSize + 1}, {Number: math.MaxInt, Size: 2}} {
		if request.Validate() == nil {
			t.Fatalf("invalid request accepted: %+v", request)
		}
	}
	for _, page := range []query.Page[int]{
		{Number: 1, Size: 1}, {Number: 7, Size: 2, Total: 1, Pages: 1},
		{Number: 1, Size: 1, Items: []int{1}}, // Insert between count and read is legal.
		{Number: 1, Size: 3, Total: math.MaxInt64, Pages: math.MaxInt64/3 + 1},
	} {
		if err := page.Validate(); err != nil {
			t.Fatalf("valid snapshot metadata rejected: %+v %v", page, err)
		}
	}
	for _, page := range []query.Page[int]{{Number: 1, Size: 1, Total: -1}, {Number: 1, Size: 1, Total: 2, Pages: 1}, {Number: 1, Size: 1, Items: []int{1, 2}}} {
		if page.Validate() == nil {
			t.Fatal("invalid page accepted")
		}
	}
	if (query.SimplePage[int]{Number: 1, Size: 2, Items: []int{1}, HasMore: true}).Validate() == nil {
		t.Fatal("short lookahead page accepted")
	}
	if err := (query.SimplePage[int]{Number: 5, Size: 2}).Validate(); err != nil {
		t.Fatal(err)
	}
}
