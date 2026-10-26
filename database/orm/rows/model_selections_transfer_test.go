package rows

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestModelSelectionsRemainOwnedThroughDerivedSources(t *testing.T) {
	d := declareMembers()
	base := d.Query()
	want, err := orm.Compile(base)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			record := ModelRecord(base)
			if _, err := CompileRead(ReadSet(Union(record, record))); err != nil {
				t.Error(err)
			}
			cursor := CursorFor(record)
			if cursor.core.Record.Source.Err != nil {
				t.Error(cursor.core.Record.Source.Err)
			}
			got, err := orm.Compile(base)
			if err != nil || got.SQL() != want.SQL() {
				t.Error("derived source changed model selections", err)
			}
		})
	}
	wg.Wait()
	// Public declarations are immutable; exercise changed copied SQL metadata at
	// its shared validator instead of manufacturing another table capability.
	definition := querycore.ModelDefinition{Table: "users", Primary: "id", Columns: []querycore.Column{{Name: "id"}, {Name: "name"}}}
	if _, err := definition.ValidateColumns(true); err != nil {
		t.Fatal(err)
	}
	copied := definition
	copied.Columns = []querycore.Column{{Name: "id"}, {Name: "other"}}
	if _, err := copied.ValidateColumns(true); err != nil {
		t.Fatal(err)
	}
	core := querycore.QueryCore{Table: "users", Model: &copied}
	s, err := core.CompileModel(querycore.ReadModels, querycore.ColumnSelections("users", copied.Columns))
	if err != nil || !strings.Contains(s.SQLText, `"other"`) || strings.Contains(s.SQLText, `"name"`) {
		t.Fatal("copied metadata reused old selections", s.SQLText, err)
	}
	copied.Columns = []querycore.Column{{Name: "id"}, {Name: "id"}}
	if _, err := copied.ValidateColumns(true); !errors.Is(err, fault.Invalid) {
		t.Fatal("copied metadata skipped validation", err)
	}
}
