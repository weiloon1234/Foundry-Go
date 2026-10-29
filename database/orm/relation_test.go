package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type relationPerson struct {
	ID        int64
	ManagerID value.Nullable[int64]
	Name      string
	DeletedAt value.Nullable[temporal.DateTime]
	Manager   relation.One[relationPerson]
	Reports   relation.Many[relationPerson]
}
type peopleTable struct {
	Table[relationPerson, int64]
	ID        Key[relationPerson, int64]
	ManagerID NullableNumber[relationPerson, int64]
	Name      Text[relationPerson, string]
	DeletedAt SoftDelete[relationPerson, temporal.DateTime]
	Manager   One[relationPerson, relationPerson]
	Reports   Many[relationPerson, relationPerson]
}

func people() peopleTable {
	return Define("people", func(b Builder[relationPerson], p *relationPerson) peopleTable {
		tbl := TableOf(b, &p.ID)
		return peopleTable{Table: tbl, ID: tbl.Key(), ManagerID: NullableNumberOf(b, &p.ManagerID), Name: TextOf(b, &p.Name), DeletedAt: SoftDeleteOf(b, &p.DeletedAt), Manager: OneOf(b, &p.Manager), Reports: ManyOf(b, &p.Reports)}
	})
}

var personColumns = []string{"id", "manager_id", "name", "deleted_at"}

func TestRelationsBatchAndFillNullableSlots(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	if err := Check(p, manager); err != nil {
		t.Fatal(err)
	}
	var statements []string
	var bindings [][]any
	closed := 0
	db := testDB(t, testConnector{statements: &statements, bindings: &bindings, onClose: func() { closed++ }, respond: func(sql string) ([]string, [][]driver.Value) {
		if strings.Contains(sql, "ANY(") {
			if closed != 1 {
				t.Fatal("parent rows not closed before eager query")
			}
			return personColumns, [][]driver.Value{{int64(2), nil, "manager", nil}}
		}
		return personColumns, [][]driver.Value{{int64(1), int64(2), "first", nil}, {int64(3), int64(2), "duplicate FK", nil}, {int64(4), nil, "no FK", nil}}
	}})
	rows, err := All(t.Context(), db, p.Query().With(manager))
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 2 || len(bindings[1]) != 2 || fmt.Sprint(bindings[1][0]) != "{2}" || bindings[1][1] != int64(10001) {
		t.Fatalf("dedup/budget %v %v", statements, bindings)
	}
	if !strings.Contains(statements[1], `"deleted_at" IS NULL`) || !strings.Contains(statements[1], `ORDER BY "people"."id" ASC`) {
		t.Fatal(statements[1])
	}
	for i, row := range rows {
		related, loaded := row.Manager.Get()
		if !loaded {
			t.Fatal("slot not loaded")
		}
		m, ok := related.Get()
		if i < 2 && (!ok || m.ID != 2) || i == 2 && ok {
			t.Fatalf("slot %d: %+v %v", i, m, ok)
		}
	}
}
func TestLoadManyCopiesParentsAndFillsDuplicateKeys(t *testing.T) {
	p := people()
	reports := HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	var statements []string
	db := testDB(t, testConnector{statements: &statements, columns: personColumns, values: [][]driver.Value{{int64(2), int64(1), "a", nil}, {int64(3), int64(1), "b", nil}}})
	parents := []relationPerson{{ID: 1}, {ID: 1}, {ID: 5}}
	loaded, err := LoadFrom(t.Context(), db, p.Query().With(reports), parents)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 1 || parents[0].Reports.IsLoaded() {
		t.Fatal("Load mutated input or used N+1")
	}
	for i, p := range loaded {
		if !p.Reports.IsLoaded() {
			t.Fatal("empty slot not loaded")
		}
		expected := 2
		if i == 2 {
			expected = 0
		}
		if p.Reports.Len() != expected {
			t.Fatalf("group %d: %d", i, p.Reports.Len())
		}
	}
	items, _ := loaded[0].Reports.Get()
	items[0].Name = "changed"
	again, _ := loaded[1].Reports.Get()
	if again[0].Name != "a" {
		t.Fatal("relation get leaked slice ownership")
	}
	statements = nil
	empty, err := LoadFrom(t.Context(), db, p.Query().With(reports), []relationPerson{})
	if err != nil || len(empty) != 0 || len(statements) != 0 {
		t.Fatal("empty parents", empty, err, statements)
	}
}
func TestRelationKeyBatchingAndSharedBudgets(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	parents := make([]relationPerson, 501)
	for i := range parents {
		parents[i].ManagerID = value.Of(int64(i + 1))
	}
	var statements []string
	var bindings [][]any
	db := testDB(t, testConnector{statements: &statements, bindings: &bindings, columns: personColumns})
	loaded, err := LoadFrom(t.Context(), db, p.Query().With(manager), parents)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 2 || len(bindings[0]) != 2 || strings.Count(fmt.Sprint(bindings[0][0]), ",") != 499 || len(bindings[1]) != 2 || len(loaded) != 501 {
		t.Fatal("batching", len(statements), len(bindings[0]), len(bindings[1]))
	}
	// Attachments count duplicates even when only one related row was fetched.
	parents = make([]relationPerson, 10001)
	for i := range parents {
		parents[i].ManagerID = value.Of(int64(2))
	}
	db = testDB(t, testConnector{columns: personColumns, values: [][]driver.Value{{int64(2), nil, "m", nil}}})
	loaded, err = LoadFrom(t.Context(), db, p.Query().With(manager), parents)
	if loaded != nil || !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "attached") {
		t.Fatal("attachment budget", loaded, err)
	}
}
func TestRelationsRejectCardinalityAndUnexpectedKeys(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	for _, tc := range []struct {
		name string
		rows [][]driver.Value
		want error
	}{
		{"singular", [][]driver.Value{{int64(2), nil, "a", nil}, {int64(2), nil, "b", nil}}, database.TooManyRows},
		{"unexpected", [][]driver.Value{{int64(9), nil, "a", nil}}, fault.Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t, testConnector{columns: personColumns, values: tc.rows})
			result, err := LoadFrom(t.Context(), db, p.Query().With(manager), []relationPerson{{ManagerID: value.Of(int64(2))}})
			if result != nil || !errors.Is(err, tc.want) {
				t.Fatal(result, err)
			}
		})
	}
}
func TestRelationStartupAndQueryOwnershipErrors(t *testing.T) {
	p, other := people(), people()
	valid := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	wrong := BelongsToOf(p.Manager, NullableNumberExpr(other.ManagerID), KeyExpr(p.ID))
	if err := Check(wrong); err == nil || !strings.Contains(err.Error(), "local key") {
		t.Fatal(err)
	}
	wrongSlot := *p.Manager.core
	wrongSlot.target = reflect.TypeFor[record]()
	mismatch := BelongsToOf(One[relationPerson, relationPerson]{core: &wrongSlot}, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	if err := Check(mismatch); err == nil || !strings.Contains(err.Error(), "slot/model mismatch") {
		t.Fatal(err)
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	for _, q := range []Query[relationPerson]{p.Query().With(nil), p.Query().With(valid, valid), other.Query().With(valid), p.Query().With(wrong)} {
		if _, err := All(t.Context(), db, q); err == nil {
			t.Fatal("invalid eager query accepted")
		}
	}
	if len(statements) != 0 {
		t.Fatal("invalid relation sent SQL")
	}
	var extra relationPerson
	escaped := OneOf(Builder[relationPerson]{}, &extra.Manager)
	if Check(BelongsToOf(escaped, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))) == nil {
		t.Fatal("ownerless slot accepted")
	}
}
func TestRelationDeclarationPointerErrors(t *testing.T) {
	var external relation.One[relationPerson]
	bad := Define("bad", func(b Builder[relationPerson], p *relationPerson) peopleTable {
		tbl := TableOf(b, &p.ID)
		OneOf(b, &external)
		return peopleTable{Table: tbl, ManagerID: NullableNumberOf(b, &p.ManagerID), Name: TextOf(b, &p.Name), DeletedAt: SoftDeleteOf(b, &p.DeletedAt), Manager: OneOf(b, &p.Manager), Reports: ManyOf(b, &p.Reports)}
	})
	if err := Check(bad); err == nil || !strings.Contains(err.Error(), "offset") {
		t.Fatal(err)
	}
}
func TestRelationTargetGlobalScopeUsesExecutionContext(t *testing.T) {
	p := people()
	p.core.model.GlobalScopes = []querycore.ScopeDefinition{{Name: "tenant", Resolve: func(ctx context.Context) (querycore.Expression, error) {
		if ctx.Value("tenant") != int64(7) {
			return nil, fmt.Errorf("missing execution context")
		}
		return querycore.PredicateNode(TextEq(p.Name, "scoped").expression), nil
	}}}
	rel := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	var statements []string
	var bindings [][]any
	db := testDB(t, testConnector{statements: &statements, bindings: &bindings, columns: personColumns})
	_, err := LoadFrom(context.WithValue(t.Context(), "tenant", int64(7)), db, p.Query().With(rel), []relationPerson{{ManagerID: value.Of(int64(2))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 1 || !strings.Contains(statements[0], `"name" =`) || !strings.Contains(fmt.Sprint(bindings), "scoped") {
		t.Fatal(statements, bindings)
	}
}

func TestRelationBudgetsAreSharedAcrossDescriptors(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	reports := HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	parents := make([]relationPerson, 6000)
	for i := range parents {
		parents[i] = relationPerson{ID: 2, ManagerID: value.Of(int64(1))}
	}
	var bindings [][]any
	db := testDB(t, testConnector{bindings: &bindings, respond: func(sql string) ([]string, [][]driver.Value) {
		return personColumns, [][]driver.Value{{int64(1), int64(2), "one related row", nil}}
	}})
	result, err := LoadFrom(t.Context(), db, p.Query().With(manager, reports), parents)
	if result != nil || !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "attached") {
		t.Fatal(result, err)
	}
	if len(bindings) != 2 || bindings[1][1] != int64(10000) {
		t.Fatal("fetched budget was reset", bindings)
	}
	if parents[0].Manager.IsLoaded() || parents[0].Reports.IsLoaded() {
		t.Fatal("partial load escaped")
	}
}
func TestSimplePageLoadsOnlyReturnedRows(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	var bindings [][]any
	db := testDB(t, testConnector{bindings: &bindings, respond: func(sql string) ([]string, [][]driver.Value) {
		if strings.Contains(sql, "ANY(") {
			return personColumns, [][]driver.Value{{int64(7), nil, "manager", nil}}
		}
		return personColumns, [][]driver.Value{{int64(1), int64(7), "shown", nil}, {int64(2), int64(8), "lookahead", nil}}
	}})
	page, err := SimplePaginate(t.Context(), db, p.Query().With(manager), Page(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Items) != 1 || len(bindings) != 2 || fmt.Sprint(bindings[1][0]) != "{7}" {
		t.Fatal(page, bindings)
	}
}

func TestLoadRejectsMixedDeclarationOwners(t *testing.T) {
	a, b := people(), people()
	manager := BelongsToOf(a.Manager, NullableNumberExpr(a.ManagerID), KeyExpr(a.ID))
	reports := HasManyOf(b.Reports, KeyExpr(b.ID), NullableNumberExpr(b.ManagerID))
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	for _, parents := range [][]relationPerson{nil, {{ID: 1}}} {
		if _, err := LoadFrom(t.Context(), db, a.Query().With(manager, reports), parents); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if len(statements) != 0 {
		t.Fatal(statements)
	}
}
