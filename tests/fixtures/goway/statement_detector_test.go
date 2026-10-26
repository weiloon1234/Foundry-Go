package goway_test

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/orm"

	"github.com/weiloon1234/Foundry-Go/database"

	"strings"
	"testing"
)

func TestCompiledStatementsCarryTrustedShapes(t *testing.T) {
	var observed []string
	{
		d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 128})
		if err != nil {
			t.Fatal(err)
		}
		ctx, scope := d.Begin(t.Context())
		g := &goldenCapture{detector: d, scopeContext: ctx}
		for range 2 {
			newGolden{g}.run()
		}
		r := scope.Report()
		total := 0
		for _, v := range r.Violations {
			if strings.HasPrefix(v.Shape, "untrusted") || strings.ContainsRune(v.Shape, 0) {
				t.Fatalf("compiled path lost its shape: %+v", v)
			}
			if len(v.CallSites) == 0 || !strings.Contains(v.CallSites[0], "goway/") {
				t.Fatalf("missing external call site: %+v", v)
			}
			total += v.Count
		}
		observed = append(observed, oracleSnapshot([]any{r.Overflow, total}))
		if r.Overflow || total != 112 {
			t.Fatalf("%d/112 statements observed: %+v", total, r)
		}
	}
	if !oracleEqual(t, "goway/statements/shapes", observed[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func TestCursorStatementsCarryTrustedShapes(t *testing.T) {
	var observed []string
	{
		d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 8})
		if err != nil {
			t.Fatal(err)
		}
		ctx, scope := d.Begin(t.Context())
		g := &goldenCapture{detector: d, scriptedColumns: readUserColumns}
		db := g.open(ctx)
		for range 2 {
			g.scriptedBatches = [][][]driver.Value{{readUser(orderID, 30, nil), readUser(buyerID, 20, "Ada")}}
			if _, err := orm.CursorPaginate(ctx, db, readUserTable().Query(), orm.CursorRequest[cursorUser]{Size: 1}); err != nil {
				t.Fatal(err)
			}
		}
		if err = db.Close(ctx); err != nil {
			t.Fatal(err)
		}
		r := scope.Report()
		if len(r.Violations) != 1 || r.Violations[0].Count != 2 || !strings.HasPrefix(r.Violations[0].Shape, "SELECT ") {
			t.Fatalf("unexpected cursor statement shape: %+v", r)
		}
		observed = append(observed, oracleSnapshot([]any{len(r.Violations), r.Violations[0].Count, strings.HasPrefix(r.Violations[0].Shape, "SELECT ")}))
	}

	if !oracleEqual(t, "goway/statements/cursor", observed[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}
