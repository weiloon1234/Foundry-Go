package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/dbassert"
	"github.com/weiloon1234/Foundry-Go/value"
)

type relationOracle struct {
	rows          [][]driver.Value
	statements    []string
	calls, closed int
	retain        bool
}

func relationRow(id int64, manager driver.Value, name string) []driver.Value {
	return []driver.Value{id, manager, name, nil}
}
func treeOracle() *relationOracle {
	return &relationOracle{retain: true, rows: [][]driver.Value{
		relationRow(1, nil, "root"), relationRow(2, nil, "root"),
		relationRow(10, int64(1), "child a"), relationRow(11, int64(1), "child b"), relationRow(12, int64(2), "child c"),
		relationRow(20, int64(10), "grand a"), relationRow(21, int64(12), "grand c"), relationRow(30, int64(20), "great"),
	}}
}
func (o *relationOracle) connector() testConnector {
	return testConnector{onClose: func() { o.closed++ }, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		if o.closed != o.calls {
			return nil, nil, errors.New("next statement began before prior rows closed")
		}
		o.calls++
		if o.retain {
			o.statements = append(o.statements, sql)
		}
		keys := map[int64]bool{}
		member := strings.Contains(sql, "ANY(")
		for _, a := range args {
			if s, ok := a.Value.(string); ok && strings.HasPrefix(s, "{") {
				for _, n := range strings.Split(strings.Trim(s, "{}"), ",") {
					id, err := strconv.ParseInt(n, 10, 64)
					if err != nil {
						return nil, nil, err
					}
					keys[id] = true
				}
			}
		}
		var result [][]driver.Value
		for _, row := range o.rows {
			index := 0
			if strings.Contains(sql, `"manager_id" = ANY(`) {
				index = 1
			}
			if member {
				id, ok := row[index].(int64)
				if !ok || !keys[id] {
					continue
				}
			}
			if strings.Contains(sql, `"name" = $`) {
				if len(args) > 0 && row[2] != args[0].Value {
					continue
				}
			}
			if strings.Contains(sql, `"name" LIKE $`) && !strings.HasPrefix(row[2].(string), strings.TrimSuffix(args[0].Value.(string), "%")) {
				continue
			}
			result = append(result, row)
		}
		sort.Slice(result, func(i, j int) bool {
			if strings.Contains(sql, `"name" DESC`) && result[i][2] != result[j][2] {
				return result[i][2].(string) > result[j][2].(string)
			}
			return result[i][0].(int64) < result[j][0].(int64)
		})
		if strings.Contains(sql, " LIMIT ") {
			if limit, ok := args[len(args)-1].Value.(int64); ok && len(result) > int(limit) {
				result = result[:limit]
			}
		}
		return personColumns, result, nil
	}}
}

func TestDirectEagerTreeStatementContract(t *testing.T) {
	p := people()
	base := HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	for depth := 1; depth <= 3; depth++ {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			for _, mode := range []string{"pool", "transaction", "locked"} {
				t.Run(mode, func(t *testing.T) {
					o := treeOracle()
					d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 4, MaxShapes: 16})
					if err != nil {
						t.Fatal(err)
					}
					db := testDB(t, o.connector(), database.WithStatementDetector(d))
					ctx, scope := d.Begin(t.Context())
					r := base
					if depth == 2 {
						r = r.With(base)
					}
					if depth == 3 {
						r = r.With(base.With(base))
					}
					q := WithRelationLimits(p.Query().Where(KeyIn(p.ID, 1, 2)), RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 3}).With(r.Where(TextStartsWith(p.Name, "child")).OrderBy(TextDesc(p.Name)))
					var parents []relationPerson
					if mode == "pool" {
						parents, err = All(ctx, db, q)
					} else {
						err = db.Transaction(ctx, func(tx *database.Tx) error {
							var e error
							if mode == "locked" {
								parents, e = AllLocked(ctx, tx, Lock(q))
							} else {
								parents, e = All(ctx, tx, q)
							}
							return e
						})
					}
					if err != nil {
						t.Fatal(err)
					}
					expected := map[int]int{1: 2, 2: 4, 3: 5}[depth]
					if o.calls != expected || o.closed != expected || len(parents) != 2 {
						t.Fatalf("depth %d: statements=%d closed=%d want=%d", depth, o.calls, o.closed, expected)
					}
					if parents[0].Reports.Len() != 2 || parents[1].Reports.Len() != 1 {
						t.Fatal("wrong direct groups")
					}
					children, _ := parents[0].Reports.Get()
					if children[0].ID != 11 || children[1].ID != 10 {
						t.Fatal("target ordering lost", children)
					}
					if depth > 1 {
						for _, parent := range parents {
							for _, child := range parent.Reports.All() {
								if !child.Reports.IsLoaded() {
									t.Fatal("nested slot not loaded")
								}
							}
						}
					}
					if depth == 3 {
						for _, child := range parents[0].Reports.All() {
							for _, grand := range child.Reports.All() {
								if !grand.Reports.IsLoaded() {
									t.Fatal("third level missing")
								}
							}
						}
					}
					if !strings.Contains(o.statements[1], `ORDER BY "people"."name" DESC, "people"."id" ASC`) {
						t.Fatal(o.statements[1])
					}
					dbassert.AssertStatementShapes(t, scope)
				})
			}
		})
	}
	if len(base.plan.query.ast.Predicates) != 0 || len(base.plan.query.ast.Orders) != 0 || len(base.plan.query.relations) != 0 {
		t.Fatal("descriptor derivation mutated declaration")
	}
}

func TestLoadMissingSkipsLoadedEmptyAndSharedNullableKeys(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	o := treeOracle()
	db := testDB(t, o.connector())
	parents := []relationPerson{
		{ID: 1, ManagerID: value.Of(int64(10)), Manager: relation.Single(value.Optional[relationPerson]{})},
		{ID: 2, ManagerID: value.Of(int64(10))},
		{ID: 3, ManagerID: value.Of(int64(11)), Manager: relation.Single(value.Set(relationPerson{ID: 11, Name: "preserved"}))},
		{ID: 4},
		{ID: 5, ManagerID: value.Of(int64(10))},
	}
	result, err := LoadMissingFrom(t.Context(), db, p.Query().With(manager.With(HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID)))), parents)
	if err != nil {
		t.Fatal(err)
	}
	if o.calls != 2 {
		t.Fatalf("missing parent/branch statements: %d", o.calls)
	} // target 10, then all target reports
	empty, _ := result[0].Manager.Get()
	if empty.IsSet() {
		t.Fatal("loaded-empty overwritten")
	}
	present, _ := result[2].Manager.Get()
	m, _ := present.Get()
	if m.Name != "preserved" {
		t.Fatal("loaded target overwritten")
	}
	for _, i := range []int{1, 4} {
		v, loaded := result[i].Manager.Get()
		m, ok := v.Get()
		if !loaded || !ok || m.ID != 10 || !m.Reports.IsLoaded() {
			t.Fatal("missing/shared FK not loaded")
		}
	}
	if !result[3].Manager.IsLoaded() || parents[1].Manager.IsLoaded() || parents[4].Manager.IsLoaded() {
		t.Fatal("input ownership or nullable key")
	}
	before := o.calls
	if _, err = LoadMissingFrom(t.Context(), db, p.Query().With(manager), result); err != nil || o.calls != before {
		t.Fatal("loaded slots queried again", err, o.calls)
	}
	refreshed, err := LoadFrom(t.Context(), db, p.Query().With(manager), parents)
	if err != nil || o.calls != before+1 {
		t.Fatal("Load did not batch refresh", err)
	}
	item, _ := refreshed[0].Manager.Get()
	if !item.IsSet() {
		t.Fatal("Load did not refresh loaded-empty")
	}
	if _, err = LoadFrom(t.Context(), db, p.Query().With(manager), []relationPerson{{ID: 1}, {ID: 2}}); err != nil || o.calls != before+1 {
		t.Fatal("empty key set sent SQL", err)
	}
}

func TestHasOneCardinalityAndNoParentStatement(t *testing.T) {
	p := people()
	r := HasOneOf(p.Manager, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	for _, id := range []int64{1, 2, 3} {
		o := treeOracle()
		db := testDB(t, o.connector())
		input := []relationPerson{{ID: id}}
		rows, err := LoadFrom(t.Context(), db, p.Query().With(r), input)
		if o.calls != 1 || input[0].Manager.IsLoaded() {
			t.Fatal("parent read or mutation")
		}
		if id == 1 {
			if rows != nil || !errors.Is(err, database.TooManyRows) {
				t.Fatal("cardinality", rows, err)
			}
			continue
		}
		if err != nil || !rows[0].Manager.IsLoaded() {
			t.Fatal(err)
		}
		item, _ := rows[0].Manager.Get()
		if item.IsSet() != (id == 2) {
			t.Fatal("singular empty/present state")
		}
	}
}

func TestRelationTreesValidateBeforeParentSQL(t *testing.T) {
	p, other := people(), people()
	r := HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	var nilRel *HasMany[relationPerson, relationPerson]
	tooDeep := r
	for range 8 {
		tooDeep = r.With(tooDeep)
	}
	limited := r
	limited.plan = copyRelationPlan(r.plan)
	limited.plan.query.ast.Limit = value.Set(1)
	offset := r
	offset.plan = copyRelationPlan(r.plan)
	offset.plan.query.ast.Offset = 1
	cases := []Query[relationPerson]{
		p.Query().With(r.With(r, r)), p.Query().With(r.OrderBy(TextAsc(p.Name), TextDesc(p.Name))), p.Query().With(tooDeep), p.Query().With(nilRel), p.Query().With(limited), p.Query().With(offset),
		p.Query().With(r.Where(TextEq(other.Name, "wrong declaration"))),
		WithRelationLimits(p.Query().With(r.With(r.With(r))), RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 2}),
		WithRelationLimits(p.Query(), RelationLimits{}),
	}
	o := treeOracle()
	db := testDB(t, o.connector())
	for _, q := range cases {
		if result, err := All(t.Context(), db, q); result != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid tree accepted", err)
		}
	}
	if o.calls != 0 {
		t.Fatal("validation sent SQL", o.calls)
	}
	captured := p.Query().Where(KeyEq(p.ID, 1)).With(&r)
	r = r.Where(TextEq(p.Name, "absent"))
	if len(captured.state.relations[0].query.ast.Predicates) != 0 {
		t.Fatal("With retained descriptor variable")
	}
}

func TestRelationTreeSharedBudgetNoPartialParents(t *testing.T) {
	p := people()
	r := HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	for _, budget := range []int{1, 3, 4} {
		o := treeOracle()
		db := testDB(t, o.connector())
		q := WithRelationLimits(p.Query().Where(KeyIn(p.ID, 1, 2)).With(r.With(r)), RelationLimits{BatchSize: 2, MaxRows: budget, MaxDepth: 3})
		if result, err := All(t.Context(), db, q); result != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("shared budget accepted partial parents", result, err)
		}
		if o.closed != o.calls {
			t.Fatal("budget error left rows open")
		}
	}
	// A failure in a later branch must leave caller-owned loaded containers intact.
	o := treeOracle()
	db := testDB(t, o.connector())
	input := []relationPerson{{ID: 1, Reports: relation.Collection([]relationPerson{{ID: 99}})}}
	single := HasOneOf(p.Manager, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	if result, err := LoadFrom(t.Context(), db, p.Query().With(r, single), input); result != nil || !errors.Is(err, database.TooManyRows) {
		t.Fatal(result, err)
	}
	children, _ := input[0].Reports.Get()
	if !reflect.DeepEqual(children, []relationPerson{{ID: 99}}) {
		t.Fatal("partial attachment published")
	}
}

func TestRelationExistenceCompilesNestedScopesWithoutRoundTrips(t *testing.T) {
	p := people()
	r := HasManyOf(p.Reports, KeyExpr(p.ID), NullableNumberExpr(p.ManagerID))
	for _, negative := range []bool{false, true} {
		predicate := Has(r, Has(r, TextEq(p.Name, "great")))
		if negative {
			predicate = Not(Has(r, Has(r, TextEq(p.Name, "great"))))
		}
		o := treeOracle()
		db := testDB(t, o.connector())
		if _, err := All(t.Context(), db, p.Query().Where(predicate)); err != nil {
			t.Fatal(err)
		}
		sql := o.statements[0]
		if o.calls != 1 || strings.Count(sql, "EXISTS(") != 2 || strings.Count(sql, `"deleted_at" IS NULL`) != 3 {
			t.Fatal(sql, o.calls)
		}
		if negative != strings.Contains(sql, "NOT ") {
			t.Fatal(sql)
		}
	}
	// Different declarations of the same Go model are still distinct owners.
	other := people()
	o := treeOracle()
	db := testDB(t, o.connector())
	for _, q := range []Query[relationPerson]{other.Query().Where(Has(r)), p.Query().Where(Has(r, TextEq(other.Name, "wrong")))} {
		if _, err := All(t.Context(), db, q); !errors.Is(err, fault.Invalid) {
			t.Fatal("wrong declaration accepted", err)
		}
	}
	if o.calls != 0 {
		t.Fatal("invalid existence sent SQL")
	}
}

func TestEagerEachAndChunkStatementWindows(t *testing.T) {
	p := people()
	manager := BelongsToOf(p.Manager, NullableNumberExpr(p.ManagerID), KeyExpr(p.ID))
	for _, mode := range []string{"each", "chunk"} {
		calls, closed := 0, 0
		db := testDB(t, testConnector{onClose: func() { closed++ }, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
			if closed != calls {
				return nil, nil, errors.New("open parent rows during eager load")
			}
			calls++
			if strings.Contains(sql, "ANY(") {
				return personColumns, [][]driver.Value{relationRow(7, nil, "manager")}, nil
			}
			if calls == 1 {
				return personColumns, [][]driver.Value{relationRow(1, int64(7), "a"), relationRow(2, int64(7), "b")}, nil
			}
			return personColumns, [][]driver.Value{relationRow(3, int64(7), "c")}, nil
		}})
		seen := 0
		q := p.Query().With(manager)
		visit := func(m relationPerson) error {
			if !m.Manager.IsLoaded() || closed != calls {
				t.Fatal("callback before complete closed eager window")
			}
			seen++
			return nil
		}
		var err error
		if mode == "each" {
			err = EachChunked(t.Context(), db, q, 2, visit)
		} else {
			err = Chunk(t.Context(), db, q, 2, func(ms []relationPerson) error {
				for _, m := range ms {
					if err := visit(m); err != nil {
						return err
					}
				}
				return nil
			})
		}
		if err != nil || seen != 3 || calls != 4 {
			t.Fatal(mode, err, seen, calls)
		}
	}
	// Default Each selects its own window size, while a bounded source avoids a
	// terminal empty window. Every related slot is complete before its callback.
	o := treeOracle()
	db := testDB(t, o.connector())
	seen := 0
	if err := Each(context.Background(), db, p.Query().Where(KeyIn(p.ID, 1, 2)).Limit(2).With(manager), func(m relationPerson) error {
		seen++
		if !m.Manager.IsLoaded() {
			t.Fatal("missing eager slot")
		}
		return nil
	}); err != nil || seen != 2 || o.calls != 1 {
		t.Fatal(err, seen, o.calls)
	}
}
