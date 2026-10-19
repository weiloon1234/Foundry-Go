package query

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestModelSelectionsRemainOwnedThroughDerivedSources(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "name"}}
	definition := Define("users", "id", columns, func(database.Row) (user, error) { return user{}, nil })
	base := ForModel(definition)
	columns[1].Name = "caller_changed"
	want, err := base.Compile()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			// Set compilation aliases its left output; a cursor source
			// aliases every inner selection. Both must preserve the base.
			if _, err := base.Union(base).Compile(); err != nil {
				t.Error(err)
			}
			if cursor := CursorFor(base); cursor.err != nil {
				t.Error(cursor.err)
			}
			got, err := base.Compile()
			if err != nil || got.SQL() != want.SQL() || strings.Contains(got.SQL(), "caller_changed") {
				t.Error("derived source mutated model selections", err)
			}
		})
	}
	wg.Wait()
	// A copied definition validates its own changed declaration and cannot
	// borrow the successful check or selection of the original owner.
	copied := *base.definition
	copied.modelDefinition.Columns = []columnData{{Name: "id"}, {Name: "other"}}
	other := ForModel(copied)
	statement, err := other.Compile()
	if err != nil || !strings.Contains(statement.SQL(), `"other"`) || strings.Contains(statement.SQL(), `"name"`) {
		t.Fatal("copied definition retained another owner's selection", err)
	}
	copied.modelDefinition.Columns = []columnData{{Name: "id"}, {Name: "id"}}
	if _, err := ForModel(copied).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("copied declaration skipped validation")
	}
}
