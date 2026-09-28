package jsonshape

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

func TestJSONFieldBudgetBeforeMetadataAllocation(t *testing.T) {
	adapter := Adapter[int]{
		NumFields: func(int) (int, bool) { return jsonwire.MaxNodes + 1, true },
		Field:     func(int, int) Field[int] { t.Fatal("over-budget metadata was accessed"); return Field[int]{} },
	}
	if _, err := Collect(0, adapter); err == nil {
		t.Fatal("over-budget shape accepted")
	}
}
