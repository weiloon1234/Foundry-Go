package goway_test

import (
	"database/sql/driver"
	"foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"strings"
	"testing"
)

func TestCompiledStatementsCarryTrustedShapes(t *testing.T) {
	var observed []string
	for _, old := range []bool{true, false} {
		d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 128})
		if err != nil {
			t.Fatal(err)
		}
		ctx, scope := d.Begin(t.Context())
		g := &goldenCapture{detector: d, scopeContext: ctx}
		for range 2 {
			if old {
				oldGolden{g}.run()
			} else {
				newGolden{g}.run()
			}
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
			t.Fatalf("old=%v: %d/112 statements observed: %+v", old, total, r)
		}
	}
	if !oracleEqual(t, "goway/statements/shapes", observed[0], observed[1]) {
		t.Fatal("statement detection parity changed")
	}
}

func TestCursorStatementsCarryTrustedShapes(t *testing.T) {
	var observed []string
	for _, old := range []bool{true, false} {
		d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 8})
		if err != nil {
			t.Fatal(err)
		}
		ctx, scope := d.Begin(t.Context())
		g := &goldenCapture{detector: d, scriptedColumns: readUserColumns}
		db := g.open(ctx)
		for range 2 {
			g.scriptedBatches = [][][]driver.Value{{readUser(orderID, 30, nil), readUser(buyerID, 20, "Ada")}}
			request := query.CursorRequest[oldmodels.User]{Size: 1}
			if old {
				_, err = oldmodels.QueryUsers().CursorPaginate(ctx, db, request)
			} else {
				_, err = orm.CursorPaginate(ctx, db, readUserTable().Query(), request)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if err = db.Close(ctx); err != nil {
			t.Fatal(err)
		}
		r := scope.Report()
		if len(r.Violations) != 1 || r.Violations[0].Count != 2 || !strings.HasPrefix(r.Violations[0].Shape, "SELECT ") {
			t.Fatalf("old=%v: %+v", old, r)
		}
		observed = append(observed, oracleSnapshot([]any{len(r.Violations), r.Violations[0].Count, strings.HasPrefix(r.Violations[0].Shape, "SELECT ")}))
	}

	if !oracleEqual(t, "goway/statements/cursor", observed[0], observed[1]) {
		t.Fatal("cursor statement detection parity changed")
	}
}
