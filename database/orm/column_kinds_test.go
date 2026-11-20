package orm

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type kindRecord struct {
	ID            int64
	Document      value.JSON[string]
	MaybeDocument value.Nullable[value.JSON[string]]
	Period        temporal.Interval
	MaybePeriod   value.Nullable[temporal.Interval]
	Name          string
	MaybeName     value.Nullable[string]
}
type kindFields struct {
	Table[kindRecord, int64]
	Document      JSON[kindRecord, string]
	MaybeDocument NullableJSON[kindRecord, string]
	Period        Interval[kindRecord]
	MaybePeriod   NullableInterval[kindRecord]
	Name          Text[kindRecord, string]
	MaybeName     NullableText[kindRecord, string]
}

func kindTable() kindFields {
	return Define("kind_records", func(b Builder[kindRecord], m *kindRecord) kindFields {
		return kindFields{TableOf(b, &m.ID), JSONOf(b, &m.Document), NullableJSONOf(b, &m.MaybeDocument), IntervalOf(b, &m.Period), NullableIntervalOf(b, &m.MaybePeriod), TextOf(b, &m.Name), NullableTextOf(b, &m.MaybeName)}
	})
}

func TestColumnKindSQLAndHydration(t *testing.T) {
	r := kindTable()
	if err := Check(r); err != nil {
		t.Fatal(err)
	}
	document, err := value.NewJSON("dark")
	if err != nil {
		t.Fatal(err)
	}
	interval := temporal.Days(2)
	statement, err := Compile(r.Query().Where(Eq(r.Document, document), Gt(r.Period, interval), IsNull(r.MaybeDocument), IsNotNull(r.MaybePeriod), IContains(r.Name, `A%_\`), IContains(r.MaybeName, `A%_\`)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement.SQL(), `"period" > $2`) || strings.Count(statement.SQL(), " ILIKE ") != 2 {
		t.Fatal(statement.SQL())
	}
	args := statement.Arguments()
	if len(args) != 4 || args[2] != `%A!%!_\%` || args[3] != args[2] {
		t.Fatalf("literal IContains bindings: %#v", args)
	}
	db := testDB(t, testConnector{columns: []string{"id", "document", "maybe_document", "period", "maybe_period", "name", "maybe_name"}, values: [][]driver.Value{{int64(1), `"dark"`, nil, interval.String(), nil, "name", nil}}})
	got, err := All(t.Context(), db, r.Query())
	if err != nil || len(got) != 1 {
		t.Fatalf("hydrate: %v", err)
	}
	if got[0].Document != document || got[0].Period != interval || !got[0].MaybeDocument.IsNull() || !got[0].MaybePeriod.IsNull() {
		t.Fatal("hydration changed values/nullness")
	}
}

func TestNewColumnAssignmentsAndMalformedRows(t *testing.T) {
	r := kindTable()
	document, err := value.NewJSON("light")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planWrite(r.Query().Where(Eq(r.Key(), 1)).state, querycore.UpdateModel, []Assignment[kindRecord]{Set(r.MaybeDocument, document), Set(r.MaybePeriod, temporal.Months(1))}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.assignments) != 2 {
		t.Fatal("nullable assignments missing")
	}
	for _, assignment := range []Assignment[kindRecord]{SetNull(r.MaybeDocument), SetNull(r.MaybePeriod)} {
		bound, err := assignment.core.Bind()
		if err != nil || bound != nil {
			t.Fatalf("SetNull: %v", err)
		}
	}
	for _, raw := range []driver.Value{`{"wrong":true}`, "invalid JSON"} {
		db := testDB(t, testConnector{columns: []string{"id", "document", "maybe_document", "period", "maybe_period", "name", "maybe_name"}, values: [][]driver.Value{{int64(1), raw, nil, "+0 mons +1 days +0 microseconds", nil, "name", nil}}})
		got, err := All(t.Context(), db, r.Query())
		if err == nil || got != nil {
			t.Fatal("malformed JSON published a model")
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[JSON[kindRecord, string]](), reflect.TypeFor[NullableJSON[kindRecord, string]](), reflect.TypeFor[Interval[kindRecord]](), reflect.TypeFor[NullableInterval[kindRecord]]()} {
		if typ.NumMethod() > 25 {
			t.Errorf("%s has %d methods", typ, typ.NumMethod())
		}
	}
}

func TestJSONContainmentUsesSharedCompiler(t *testing.T) {
	r := kindTable()
	v, err := value.NewJSON("dark")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compile(r.Query().Where(ContainsDocument(r.Document, v), ContainedByDocument(r.MaybeDocument, v)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.SQL(), " @> ") || !strings.Contains(s.SQL(), " <@ ") {
		t.Fatal(s.SQL())
	}
	if !reflect.DeepEqual(s.Arguments(), []any{`"dark"`, true, `"dark"`, true}) {
		t.Fatalf("containment bindings: %#v", s.Arguments())
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	if _, err = Count(t.Context(), db, r.Query().Where(ContainsDocument(r.Document, value.JSON[string]{}))); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if len(statements) != 0 {
		t.Fatal("invalid JSON sent SQL")
	}
	var unbound NullableJSON[kindRecord, string]
	if _, err = Compile(r.Query().Where(ContainsDocument(unbound, v))); err == nil {
		t.Fatal("unbound nullable JSON accepted")
	}
}

func TestJSONOrderingAndNullUseSharedCompiler(t *testing.T) {
	r := kindTable()
	s, err := Compile(r.Query().Where(IsJSONNull(r.Document), IsJSONNull(r.MaybeDocument)).OrderBy(Asc(r.Document), Desc(r.MaybeDocument)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(s.SQL(), "JSONB_TYPEOF(") != 2 || !strings.Contains(s.SQL(), `ORDER BY "kind_records"."document" ASC, "kind_records"."maybe_document" DESC`) {
		t.Fatal(s.SQL())
	}
	if !reflect.DeepEqual(s.Arguments(), []any{"null", "null"}) {
		t.Fatalf("JSON kind bindings: %#v", s.Arguments())
	}
	var unbound JSON[kindRecord, string]
	var nullable NullableJSON[kindRecord, string]
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	for _, predicate := range []Predicate[kindRecord]{IsJSONNull(unbound), IsJSONNull(nullable)} {
		if _, err := Count(t.Context(), db, r.Query().Where(predicate)); err == nil {
			t.Fatal("unbound JSON null predicate accepted")
		}
	}
	if len(statements) != 0 {
		t.Fatal("unbound JSON sent SQL")
	}
}

// Frozen legacy signatures preserve value/nullability and result capabilities
// across unified package operator functions on methodless ColumnHandle types.
// Advanced operations remain separate expr/rows package functions.
func TestColumnKindCoreParity(t *testing.T) {
	packageOperations := map[string]string{
		"EqColumn": "6b", "NeColumn": "6b", "GtColumn": "6b", "GteColumn": "6b", "LtColumn": "6b", "LteColumn": "6b",
		"Group": "6a", "InQuery": "6b", "InNullableQuery": "6b", "InCorrelatedQuery": "6b", "InNullableCorrelatedQuery": "6b", "Incoming": "5",
		"Kind": "6d", "Length": "6d",
	}
	cases := []struct {
		name string
		typ  reflect.Type
		ops  map[string]any
	}{
		{
			name: "Column[github.com/weiloon1234/Foundry-Go/database/orm.kindRecord,github.com/weiloon1234/Foundry-Go/auth/password.Hash]",
			typ:  reflect.TypeFor[Column[kindRecord, password.Hash]](),
			ops: map[string]any{
				"Asc":   Asc[kindRecord, password.Hash, password.Hash, password.Hash, RequiredScalarKind, OrdinaryRole],
				"Desc":  Desc[kindRecord, password.Hash, password.Hash, password.Hash, RequiredScalarKind, OrdinaryRole],
				"Eq":    Eq[kindRecord, password.Hash, password.Hash, password.Hash, RequiredScalarKind, OrdinaryRole],
				"In":    In[kindRecord, password.Hash, password.Hash, password.Hash, RequiredScalarKind, OrdinaryRole],
				"Ne":    Ne[kindRecord, password.Hash, password.Hash, password.Hash, RequiredScalarKind, OrdinaryRole],
				"NotIn": NotIn[kindRecord, password.Hash, password.Hash, password.Hash, RequiredScalarKind, OrdinaryRole],
				"Set":   Set[kindRecord, password.Hash, password.Hash, password.Hash, RequiredScalarKind, OrdinaryRole],
			},
		},
		{
			name: "NullableColumn[github.com/weiloon1234/Foundry-Go/database/orm.kindRecord,github.com/weiloon1234/Foundry-Go/auth/password.Hash]",
			typ:  reflect.TypeFor[NullableColumn[kindRecord, password.Hash]](),
			ops: map[string]any{
				"Asc":       Asc[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"Desc":      Desc[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"Eq":        Eq[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"In":        In[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"IsNotNull": IsNotNull[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"IsNull":    IsNull[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"Ne":        Ne[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"NotIn":     NotIn[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"Set":       Set[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
				"SetNull":   SetNull[kindRecord, password.Hash, password.Hash, value.Nullable[password.Hash], NullableScalarKind, OrdinaryRole],
			},
		},
		{
			name: "Interval[github.com/weiloon1234/Foundry-Go/database/orm.kindRecord]",
			typ:  reflect.TypeFor[Interval[kindRecord]](),
			ops: map[string]any{
				"Asc":     Asc[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Between": Between[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Desc":    Desc[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Eq":      Eq[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Gt":      Gt[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Gte":     Gte[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"In":      In[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Lt":      Lt[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Lte":     Lte[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Ne":      Ne[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"NotIn":   NotIn[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
				"Set":     Set[kindRecord, temporal.Interval, temporal.Interval, temporal.Interval, RequiredIntervalKind, OrdinaryRole],
			},
		},
		{
			name: "NullableInterval[github.com/weiloon1234/Foundry-Go/database/orm.kindRecord]",
			typ:  reflect.TypeFor[NullableInterval[kindRecord]](),
			ops: map[string]any{
				"Asc":       Asc[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Between":   Between[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Desc":      Desc[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Eq":        Eq[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Gt":        Gt[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Gte":       Gte[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"In":        In[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"IsNotNull": IsNotNull[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"IsNull":    IsNull[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Lt":        Lt[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Lte":       Lte[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Ne":        Ne[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"NotIn":     NotIn[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"Set":       Set[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
				"SetNull":   SetNull[kindRecord, temporal.Interval, temporal.Interval, value.Nullable[temporal.Interval], NullableIntervalKind, OrdinaryRole],
			},
		},
		{
			name: "JSON[github.com/weiloon1234/Foundry-Go/database/orm.kindRecord,string]",
			typ:  reflect.TypeFor[JSON[kindRecord, string]](),
			ops: map[string]any{
				"Asc":         Asc[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"ContainedBy": ContainedByDocument[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"Contains":    ContainsDocument[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"Desc":        Desc[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"Eq":          Eq[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"In":          In[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"IsJSONNull":  IsJSONNull[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"Ne":          Ne[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"NotIn":       NotIn[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
				"Set":         Set[kindRecord, value.JSON[string], value.JSON[string], value.JSON[string], RequiredJSONKind, OrdinaryRole],
			},
		},
		{
			name: "NullableJSON[github.com/weiloon1234/Foundry-Go/database/orm.kindRecord,string]",
			typ:  reflect.TypeFor[NullableJSON[kindRecord, string]](),
			ops: map[string]any{
				"Asc":         Asc[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"ContainedBy": ContainedByDocument[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"Contains":    ContainsDocument[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"Desc":        Desc[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"Eq":          Eq[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"In":          In[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"IsJSONNull":  IsJSONNull[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"IsNotNull":   IsNotNull[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"IsNull":      IsNull[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"Ne":          Ne[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"NotIn":       NotIn[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"Set":         Set[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
				"SetNull":     SetNull[kindRecord, value.JSON[string], value.JSON[string], value.Nullable[value.JSON[string]], NullableJSONKind, OrdinaryRole],
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.typ.NumMethod() != 0 {
				t.Fatalf("%s has %d methods; declaration columns must be methodless", tc.typ, tc.typ.NumMethod())
			}
			signatures := func() map[string]string {
				result := map[string]string{}
				families := map[reflect.Type]string{
					reflect.TypeFor[Predicate[kindRecord]]():  "Predicate",
					reflect.TypeFor[Order[kindRecord]]():      "Order",
					reflect.TypeFor[Assignment[kindRecord]](): "Assignment",
				}
				for name, fn := range tc.ops {
					if stage := packageOperations[name]; stage != "" {
						t.Errorf("%s belongs to the Stage %s expression surface, not core column operators", name, stage)
						continue
					}
					sig := reflect.TypeOf(fn)
					if sig.Kind() != reflect.Func || sig.NumIn() < 1 || sig.In(0) != tc.typ {
						t.Fatalf("operator %s on %s has unexpected column parameter in %s", name, tc.typ, sig)
					}
					shape := fmt.Sprintf("inputs=%d outputs=%d variadic=%t", sig.NumIn()-1, sig.NumOut(), sig.IsVariadic())
					for j := 1; j < sig.NumIn(); j++ {
						shape += fmt.Sprintf(" in:%s:%s", sig.In(j).Kind(), sig.In(j))
					}
					for j := 0; j < sig.NumOut(); j++ {
						family, ok := families[sig.Out(j)]
						if !ok {
							t.Fatalf("unmapped result family %s for %s", sig.Out(j), name)
						}
						shape += fmt.Sprintf(" out:%s:%s", sig.Out(j).Kind(), family)
					}
					result[name] = shape
				}
				return result
			}
			frozenMatch(t, "signatures", signatures())
		})
	}
}
