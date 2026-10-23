package orm

import (
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/testkit/dbassert"
)

func TestStatementDetectorFindsLoopButAcceptsEagerBatch(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, p.ManagerID, p.ID)
	d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 2, MaxShapes: 16})
	db := testDB(t, testConnector{respond: func(sql string) ([]string, [][]driver.Value) {
		if strings.Contains(sql, "ANY(") || strings.Contains(sql, `"id" =`) {
			return personColumns, [][]driver.Value{{int64(2), nil, "manager", nil}}
		}
		return personColumns, [][]driver.Value{{int64(1), int64(2), "a", nil}, {int64(3), int64(2), "b", nil}, {int64(4), int64(2), "c", nil}}
	}}, database.WithStatementDetector(d))
	ctx, scope := d.Begin(t.Context())
	parents, err := p.Query().All(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for range parents {
		if _, err = p.Require(ctx, db, 2); err != nil {
			t.Fatal(err)
		}
	}
	if r := scope.Report(); len(r.Violations) != 1 || r.Violations[0].Count != 3 || !strings.HasPrefix(r.Violations[0].Shape, "SELECT ") {
		t.Fatal(r)
	}
	ctx, scope = d.Begin(t.Context())
	parents, err = p.Query().With(manager).All(ctx, db)
	if err != nil || len(parents) != 3 {
		t.Fatal(err)
	}
	for _, parent := range parents {
		related, loaded := parent.Manager.Get()
		m, present := related.Get()
		if !loaded || !present || m.ID != 2 {
			t.Fatal("batch did not hydrate")
		}
	}
	dbassert.AssertStatementShapes(t, scope)
}
