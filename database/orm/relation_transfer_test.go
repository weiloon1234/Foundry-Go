package orm

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestOneOfManyLoadsFirstRowPerKey(t *testing.T) {
	d := people()
	base := BelongsToOf(d.Manager, d.ManagerID, d.ID)
	r := base.OfMany(d.Name.Desc())
	n, e := relationSelect(r.plan, t.Context())
	if e != nil {
		t.Fatal(e)
	}
	s, e := querycore.CompileRead(n, t.Context(), false, nil, true)
	if e != nil || !strings.Contains(s.SQLText, `SELECT DISTINCT ON ("people"."id")`) || !strings.Contains(s.SQLText, `ORDER BY "people"."id" ASC, "people"."name" DESC`) {
		t.Fatal(s.SQLText, e)
	}
	n, e = relationSelect(base.LatestOfMany().plan, t.Context())
	if e != nil {
		t.Fatal(e)
	}
	s, e = querycore.CompileRead(n, t.Context(), false, nil, true)
	if e != nil || !strings.Contains(s.SQLText, `"people"."id" DESC`) {
		t.Fatal(s.SQLText, e)
	}
	if e := validateQuery(d.Query().With(base.OfMany()).state); !errors.Is(e, fault.Invalid) {
		t.Fatal("unordered one-of-many", e)
	}
	if e := validateAggregateInput(r.aggregateInput(nil, nil), d.core); !errors.Is(e, fault.Invalid) {
		t.Fatal("aggregate over one-of-many", e)
	}
}
func TestRelationBindingsAndCapturedValues(t *testing.T) {
	d := scopeTransferFields()
	base := HasManyOf(d.Children, d.Key(), d.Key())
	r := base
	q := d.Query().With(&r)
	r = r.Where(d.Key().Eq(1))
	if len(q.state.relations[0].query.ast.Predicates) != 0 {
		t.Fatal("captured mutable descriptor")
	}
	if e := validateQuery(d.Query().With(r).state); e != nil {
		t.Fatal(e)
	}
	var missing *HasMany[scopeTransferRow, scopeTransferRow]
	foreign := scopeTransferFields()
	for _, q := range []Query[scopeTransferRow]{d.Query().With(missing), d.Query().With(r, r), d.Query().With(HasManyOf(d.Children, foreign.Key(), d.Key())), d.Query().With(HasManyOf(d.Children, Primary[scopeTransferRow, int64]{}, d.Key()))} {
		if e := validateQuery(q.state); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid binding", e)
		}
	}
	nested := base
	for range 9 {
		nested = base.With(nested)
	}
	if e := validateQuery(d.Query().With(nested).state); !errors.Is(e, fault.Invalid) {
		t.Fatal("default depth", e)
	}
	limits := DefaultRelationLimits()
	limits.MaxDepth = 12
	if e := validateQuery(WithRelationLimits(d.Query(), limits).With(nested).state); e != nil {
		t.Fatal(e)
	}
}
func TestThroughAliasedScopesAndStableOrder(t *testing.T) {
	d := scopeTransferFields()
	base := ManyToManyOf(d.Links, d.Key(), d.Key(), d.Key(), d.Key())
	r := base.Where(Or(d.Key().Eq(1), Not(d.Rank.Gt(2)))).WherePivot(d.Rank.Gte(3)).OrderByPivot(d.Rank.Desc()).OrderBy(d.Rank.Asc())
	if e := validateQuery(d.Query().With(r).state); e != nil {
		t.Fatal(e)
	}
	n, e := relationSelect(r.plan, t.Context())
	if e != nil {
		t.Fatal(e)
	}
	n.Predicates = append(n.Predicates, querycore.Requalify(querycore.PredicateNode(d.Key().In(4, 5).expression), querycore.RelationPivotAlias))
	n.Limit = value.Set(6)
	s, e := querycore.CompileRead(n, t.Context(), false, nil, true)
	for _, want := range []string{`FROM "scope_records" AS "foundry_target" INNER JOIN "scope_records" AS "foundry_pivot" ON ("foundry_target"."id" = "foundry_pivot"."id")`, `WHERE (("foundry_target"."id" = $1) OR (NOT ("foundry_target"."rank" > $2))) AND ("foundry_pivot"."rank" >= $3) AND ("foundry_pivot"."id" = ANY($4))`, `ORDER BY "foundry_pivot"."rank" DESC, "foundry_target"."rank" ASC, "foundry_target"."id" ASC, "foundry_pivot"."id" ASC LIMIT $5`} {
		if !strings.Contains(s.SQLText, want) {
			t.Fatal(want, s.SQLText, e)
		}
	}
	if e != nil || !reflect.DeepEqual(s.Bindings, []any{int64(1), int64(2), int64(3), "{4,5}", int64(6)}) {
		t.Fatal(s.Bindings, e)
	}
	if len(base.plan.join.orders) != 0 || len(base.plan.query.ast.Predicates) != 0 || len(base.plan.join.pivot.ast.Predicates) != 0 {
		t.Fatal("base mutated")
	}
	q := d.Query().With(&r)
	r = r.WherePivot(d.Key().Eq(9))
	if len(q.state.relations[0].join.pivot.ast.Predicates) != 1 {
		t.Fatal("captured pointer changed")
	}
}
func TestThroughInvalidDeclarations(t *testing.T) {
	d, foreign := scopeTransferFields(), scopeTransferFields()
	r := ManyToManyOf(d.Links, d.Key(), d.Key(), d.Key(), d.Key())
	var missing *ThroughRelation[scopeTransferRow, scopeTransferRow, scopeTransferRow]
	bad := d.Key().Eq(1)
	bad.expression = querycore.RetainPredicate(querycore.Comparison{Operand: querycore.FieldRef{Table: "scope_records", Column: "missing"}, Operator: querycore.IsNull})
	for _, q := range []Query[scopeTransferRow]{d.Query().With(missing), d.Query().With(r.WherePivot(bad)), d.Query().With(r.WherePivot(foreign.Key().Eq(1))), d.Query().With(r.OrderBy(foreign.Key().Asc())), d.Query().With(r.OrderBy(d.Key().Asc(), d.Key().Desc())), d.Query().With(r.OrderByPivot(foreign.Key().Asc())), d.Query().With(ManyToManyOf(d.Links, Primary[scopeTransferRow, int64]{}, d.Key(), d.Key(), d.Key()))} {
		if e := validateQuery(q.state); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid through", e)
		}
	}
	wide := make([]Predicate[scopeTransferRow], querycore.MaxExpressionNodes/3+1)
	for i := range wide {
		wide[i] = d.Key().Eq(1)
	}
	if e := validateQuery(d.Query().With(r.Where(wide...)).state); e != nil {
		t.Fatal("legal half", e)
	}
	if e := validateQuery(d.Query().With(r.Where(wide...).WherePivot(wide...)).state); !errors.Is(e, fault.Invalid) {
		t.Fatal("combined bound", e)
	}
}
func TestRelationWriteKeysPreserveStoredTypesAndNullability(t *testing.T) {
	d := people()
	id := pivotColumn(d.core, "id")
	nullable := pivotColumn(d.core, "manager_id")
	m := relationPerson{ID: 4}
	key, e := pivotKey(id, unsafe.Pointer(&m))
	if e != nil {
		t.Fatal(e)
	}
	defs, e := relationPivotDefaults(nil, id, key.raw)
	if e != nil {
		t.Fatal(e)
	}
	defs, e = relationPivotDefaults(defs, nullable, int64(8))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(defs[0].InputValue(), int64(4)) || !reflect.DeepEqual(defs[1].InputValue(), value.Of(int64(8))) {
		t.Fatal("stored defaults lost type")
	}
	q := d.Query()
	q.state.ast.Predicates = []querycore.Expression{pivotKeyPredicate(id, []pivotWriteKey{key}, false), pivotKeyPredicate(nullable, []pivotWriteKey{{raw: int64(8)}}, false)}
	s, e := Compile(q)
	if e != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(4), int64(8)}) {
		t.Fatal(s.Arguments(), e)
	}
	if _, e := pivotKey(nullable, unsafe.Pointer(&m)); !errors.Is(e, fault.Missing) {
		t.Fatal("NULL endpoint", e)
	}
}
func TestRelationWriteBoundsAndUnsupportedReadOptions(t *testing.T) {
	_, z, p, r := writeRelationTables()
	limited := WithRelationWriteLimit(r, 2)
	if n, e := validateRelationWrite(limited.plan, pivotAttach); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	if n, e := validateRelationWrite(r.plan, pivotAttach); e != nil || n != MaxRelationWriteRows || r.plan.writeLimit != 0 {
		t.Fatal("descriptor changed", n, e)
	}
	for _, bad := range []ThroughRelation[writeParent, writeTarget, writePivot]{WithRelationWriteLimit(r, 0), WithRelationWriteLimit(r, MaxRelationWriteRows+1), r.OrderBy(z.ID.Asc()), r.OrderByPivot(p.ID.Asc()), {}} {
		if _, e := validateRelationWrite(bad.plan, pivotAttach); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid write", e)
		}
	}
	for _, mode := range []string{"children", "pivot children", "same key", "hop"} {
		p := copyRelationPlan(r.plan)
		j := *p.join
		p.join = &j
		switch mode {
		case "children":
			p.query.relations = []*relationPlan{r.plan}
		case "pivot children":
			p.join.pivot.relations = []*relationPlan{r.plan}
		case "same key":
			p.join.second = p.join.first
		case "hop":
			p.join.hop = true
		}
		if _, e := validateRelationWrite(p, pivotAttach); !errors.Is(e, fault.Invalid) {
			t.Fatal(mode, e)
		}
	}
	if _, e := validateRelationWrite(r.Where(z.ID.Eq(3)).WherePivot(p.ID.Eq(4)).plan, pivotAttach); e != nil {
		t.Fatal("valid scopes", e)
	}
}
func TestInvalidRelationWriteFailsBeforeTransaction(t *testing.T) {
	a, z, p, r := writeRelationTables()
	w := &noEachWriter{}
	if _, e := Attach(t.Context(), w, r, writeParent{}, writeTarget{}, Assignment[writePivot]{}); !errors.Is(e, fault.Invalid) {
		t.Fatal(e)
	}
	if _, e := Detach(t.Context(), w, WithRelationWriteLimit(r, 0), writeParent{}, writeTarget{}); !errors.Is(e, fault.Invalid) {
		t.Fatal(e)
	}
	invalidDefaults := [][]Assignment[writePivot]{{p.ID.Set(1), p.ID.Set(2)}}
	_, _, foreign, _ := writeRelationTables()
	invalidDefaults = append(invalidDefaults, []Assignment[writePivot]{foreign.ID.Set(1)})
	bad := p.ID.Set(1)
	bad.core.Field.Column = "missing"
	invalidDefaults = append(invalidDefaults, []Assignment[writePivot]{bad})
	for _, as := range invalidDefaults {
		if _, e := Attach(t.Context(), w, r, writeParent{}, writeTarget{}, as...); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid defaults", e)
		}
	}
	noSoft := Define("plain_pivots", func(b Builder[writePivot], m *writePivot) writePivotTable {
		return writePivotTable{Table: TableOf(b, &m.ID), ParentID: NumberOf(b, &m.ParentID), TargetID: NumberOf(b, &m.TargetID), Role: TextOf(b, &m.Role)}
	}, NoSoftDeletes())
	plain := ManyToManyOf(a.Links, a.ID, noSoft.ParentID, noSoft.TargetID, z.ID)
	if _, e := validateRelationWrite(plain.plan, pivotForceDetach); !errors.Is(e, fault.Invalid) {
		t.Fatal("force detach plain pivot", e)
	}
	if w.calls != 0 {
		t.Fatal("invalid write acquired transaction", w.calls)
	}
}
func TestRelationPredicatesNestWithoutAliasCapture(t *testing.T) {
	d := scopeTransferFields()
	base := HasManyOf(d.Children, d.Key(), d.Key())
	r := base.Where(d.Key().Eq(7))
	for range 5 {
		r = base.Where(Has(r))
	}
	q := d.Query().Where(Has(r), Not(Has(base.Where(d.Key().Eq(9)))))
	s, e := Compile(q)
	if e != nil || strings.Count(s.SQL(), "EXISTS(") != 7 || !strings.Contains(s.SQL(), `AS "foundry_related_6"`) || !reflect.DeepEqual(s.Arguments(), []any{int64(7), int64(9)}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	r = r.Where(d.Key().Eq(11))
	again, e := Compile(q)
	if e != nil || again.SQL() != s.SQL() || !reflect.DeepEqual(again.Arguments(), s.Arguments()) {
		t.Fatal("descriptor mutated", e)
	}
	node := d.Query().state.ast.ModelSelect(d.core.selections)
	node.Source.Alias = "foundry_related"
	node.Selections = querycore.ColumnSelections("foundry_related", d.core.model.Columns)
	node.Predicates = []querycore.Expression{querycore.BinaryComparison{Left: querycore.FieldRef{Table: "foundry_related", Column: "id"}, Right: querycore.FieldRef{Table: "scope_records", Column: "id"}, Operator: querycore.Equal}}
	manual := Predicate[scopeTransferRow]{owner: d.core, expression: querycore.RetainPredicate(querycore.SubqueryPredicate{Query: querycore.Subquery{Node: node, Correlation: &querycore.ScopeRequirement{Sources: map[string][]querycore.Column{"scope_records": d.core.model.Columns}}}})}
	through := ManyToManyOf(d.Links, d.Key(), d.Key(), d.Key(), d.Key())
	for _, p := range []Predicate[scopeTransferRow]{Has(base.Where(manual)), Has(through.Where(manual).WherePivot(Has(base)))} {
		if s, e := Compile(d.Query().Where(p)); e != nil {
			t.Fatal("manual alias captured", s.SQL(), e)
		}
	}
}
func TestRelationPredicatesUseOnlyFilterScopes(t *testing.T) {
	d := scopeTransferFields()
	base := HasManyOf(d.Children, d.Key(), d.Key())
	r := base.Where(d.Key().Eq(2)).OrderBy(d.Key().Desc()).With(base)
	s, e := Compile(d.Query().Where(Has(r)))
	if e != nil || strings.Contains(s.SQL(), "ORDER BY") || strings.Count(s.SQL(), "SELECT") != 2 || !reflect.DeepEqual(s.Arguments(), []any{int64(2)}) {
		t.Fatal(s.SQL(), e)
	}
	if len(r.plan.query.ast.Orders) != 1 || len(r.plan.query.relations) != 1 {
		t.Fatal("reusable descriptor stripped")
	}
	window := base
	window.plan = copyRelationPlan(base.plan)
	window.plan.query.ast.Limit = value.Set(1)
	unknown := base
	unknown.plan = copyRelationPlan(base.plan)
	unknown.plan.query.ast.Predicates = []querycore.Expression{querycore.Comparison{Operand: querycore.FieldRef{Table: "scope_records", Column: "missing"}, Operator: querycore.IsNull}}
	var missing *HasMany[scopeTransferRow, scopeTransferRow]
	for _, p := range []Predicate[scopeTransferRow]{Has(missing), Not(Has[scopeTransferRow, scopeTransferRow](nil)), Has(HasMany[scopeTransferRow, scopeTransferRow]{}), Has(ThroughRelation[scopeTransferRow, scopeTransferRow, scopeTransferRow]{}), Has(window), Has(unknown)} {
		s, e := Compile(d.Query().Where(p))
		if !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid relation compiled", e)
		}
	}
	node := d.Query().state.ast.ModelSelect(d.core.selections)
	node.Predicates = make([]querycore.Expression, 1)
	node.Predicates[0] = querycore.SubqueryPredicate{Query: querycore.Subquery{Node: node}}
	cyclic := base
	cyclic.plan = copyRelationPlan(base.plan)
	cyclic.plan.query.ast.Predicates = node.Predicates
	if _, e := Compile(d.Query().Where(Has(cyclic))); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "resource bound") {
		t.Fatal("alias cycle", e)
	}
}

type relationTransferFailScan struct{ err error }

func (s relationTransferFailScan) Scan(any) error { return s.err }
func TestJoinedHydrationPublishesOnlyCompleteTuple(t *testing.T) {
	for _, fail := range []bool{false, true} {
		a, _, p, r := writeRelationTables()
		sentinel := errors.New("second decoder failed")
		if fail {
			p.core.columns[0].scan = func(unsafe.Pointer) any { return relationTransferFailScan{sentinel} }
		}
		closed := 0
		db := testDB(t, testConnector{onClose: func() { closed++ }, columns: make([]string, 7), values: [][]driver.Value{{int64(10), int64(20), int64(100), int64(1), int64(10), "member", nil}}})
		parents := []writeParent{{ID: 1}}
		got, e := LoadFrom(t.Context(), db, a.Query().With(r), parents)
		if fail {
			if !errors.Is(e, sentinel) || got != nil {
				t.Fatal("partial tuple escaped", got, e)
			}
		} else {
			if e != nil || len(got) != 1 {
				t.Fatal(got, e)
			}
			links, loaded := got[0].Links.Get()
			if !loaded || len(links) != 1 || links[0].Model.ID != 10 || links[0].Pivot.ID != 100 {
				t.Fatal("partition", links)
			}
		}
		if parents[0].Links.IsLoaded() || closed != 1 {
			t.Fatal("input/closure", closed)
		}
	}
}
