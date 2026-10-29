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
	statement, err := Compile(r.Query().Where(JSONEq(r.Document, document), IntervalGt(r.Period, interval), NullableJSONIsNull(r.MaybeDocument), NullableIntervalIsNotNull(r.MaybePeriod), TextIContains(r.Name, `A%_\`), NullableTextIContains(r.MaybeName, `A%_\`)))
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
	plan, err := planWrite(r.Query().Where(KeyEq(r.Key(), 1)).state, querycore.UpdateModel, []Assignment[kindRecord]{NullableJSONSet(r.MaybeDocument, document), NullableIntervalSet(r.MaybePeriod, temporal.Months(1))}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.assignments) != 2 {
		t.Fatal("nullable assignments missing")
	}
	for _, assignment := range []Assignment[kindRecord]{NullableJSONSetNull(r.MaybeDocument), NullableIntervalSetNull(r.MaybePeriod)} {
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
	s, err := Compile(r.Query().Where(JSONContains(r.Document, v), NullableJSONContainedBy(r.MaybeDocument, v)))
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
	if _, err = Count(t.Context(), db, r.Query().Where(JSONContains(r.Document, value.JSON[string]{}))); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if len(statements) != 0 {
		t.Fatal("invalid JSON sent SQL")
	}
	var unbound NullableJSON[kindRecord, string]
	if _, err = Compile(r.Query().Where(NullableJSONContains(unbound, v))); err == nil {
		t.Fatal("unbound nullable JSON accepted")
	}
}

func TestJSONOrderingAndNullUseSharedCompiler(t *testing.T) {
	r := kindTable()
	s, err := Compile(r.Query().Where(JSONIsJSONNull(r.Document), NullableJSONIsJSONNull(r.MaybeDocument)).OrderBy(JSONAsc(r.Document), NullableJSONDesc(r.MaybeDocument)))
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
	for _, predicate := range []Predicate[kindRecord]{JSONIsJSONNull(unbound), NullableJSONIsJSONNull(nullable)} {
		if _, err := Count(t.Context(), db, r.Query().Where(predicate)); err == nil {
			t.Fatal("unbound JSON null predicate accepted")
		}
	}
	if len(statements) != 0 {
		t.Fatal("unbound JSON sent SQL")
	}
}

// Frozen legacy signatures preserve value/nullability and result capabilities
// across D2's per-kind package operator functions on methodless column types.
// Advanced operations remain separate expr/rows package functions.
func TestColumnKindCoreParity(t *testing.T) {
	packageOperations := map[string]string{
		"EqColumn": "6b", "NeColumn": "6b", "GtColumn": "6b", "GteColumn": "6b", "LtColumn": "6b", "LteColumn": "6b",
		"Group": "6a", "InQuery": "6b", "InNullableQuery": "6b", "InCorrelatedQuery": "6b", "InNullableCorrelatedQuery": "6b", "Incoming": "5",
		"Kind": "6d", "Length": "6d",
	}
	cases := []struct {
		typ reflect.Type
		ops map[string]any
	}{
		{
			typ: reflect.TypeFor[Column[kindRecord, password.Hash]](),
			ops: map[string]any{
				"Asc":   ColumnAsc[kindRecord, password.Hash],
				"Desc":  ColumnDesc[kindRecord, password.Hash],
				"Eq":    ColumnEq[kindRecord, password.Hash],
				"In":    ColumnIn[kindRecord, password.Hash],
				"Ne":    ColumnNe[kindRecord, password.Hash],
				"NotIn": ColumnNotIn[kindRecord, password.Hash],
				"Set":   ColumnSet[kindRecord, password.Hash],
			},
		},
		{
			typ: reflect.TypeFor[NullableColumn[kindRecord, password.Hash]](),
			ops: map[string]any{
				"Asc":       NullableColumnAsc[kindRecord, password.Hash],
				"Desc":      NullableColumnDesc[kindRecord, password.Hash],
				"Eq":        NullableColumnEq[kindRecord, password.Hash],
				"In":        NullableColumnIn[kindRecord, password.Hash],
				"IsNotNull": NullableColumnIsNotNull[kindRecord, password.Hash],
				"IsNull":    NullableColumnIsNull[kindRecord, password.Hash],
				"Ne":        NullableColumnNe[kindRecord, password.Hash],
				"NotIn":     NullableColumnNotIn[kindRecord, password.Hash],
				"Set":       NullableColumnSet[kindRecord, password.Hash],
				"SetNull":   NullableColumnSetNull[kindRecord, password.Hash],
			},
		},
		{
			typ: reflect.TypeFor[Interval[kindRecord]](),
			ops: map[string]any{
				"Asc":     IntervalAsc[kindRecord],
				"Between": IntervalBetween[kindRecord],
				"Desc":    IntervalDesc[kindRecord],
				"Eq":      IntervalEq[kindRecord],
				"Gt":      IntervalGt[kindRecord],
				"Gte":     IntervalGte[kindRecord],
				"In":      IntervalIn[kindRecord],
				"Lt":      IntervalLt[kindRecord],
				"Lte":     IntervalLte[kindRecord],
				"Ne":      IntervalNe[kindRecord],
				"NotIn":   IntervalNotIn[kindRecord],
				"Set":     IntervalSet[kindRecord],
			},
		},
		{
			typ: reflect.TypeFor[NullableInterval[kindRecord]](),
			ops: map[string]any{
				"Asc":       NullableIntervalAsc[kindRecord],
				"Between":   NullableIntervalBetween[kindRecord],
				"Desc":      NullableIntervalDesc[kindRecord],
				"Eq":        NullableIntervalEq[kindRecord],
				"Gt":        NullableIntervalGt[kindRecord],
				"Gte":       NullableIntervalGte[kindRecord],
				"In":        NullableIntervalIn[kindRecord],
				"IsNotNull": NullableIntervalIsNotNull[kindRecord],
				"IsNull":    NullableIntervalIsNull[kindRecord],
				"Lt":        NullableIntervalLt[kindRecord],
				"Lte":       NullableIntervalLte[kindRecord],
				"Ne":        NullableIntervalNe[kindRecord],
				"NotIn":     NullableIntervalNotIn[kindRecord],
				"Set":       NullableIntervalSet[kindRecord],
				"SetNull":   NullableIntervalSetNull[kindRecord],
			},
		},
		{
			typ: reflect.TypeFor[JSON[kindRecord, string]](),
			ops: map[string]any{
				"Asc":         JSONAsc[kindRecord, string],
				"ContainedBy": JSONContainedBy[kindRecord, string],
				"Contains":    JSONContains[kindRecord, string],
				"Desc":        JSONDesc[kindRecord, string],
				"Eq":          JSONEq[kindRecord, string],
				"In":          JSONIn[kindRecord, string],
				"IsJSONNull":  JSONIsJSONNull[kindRecord, string],
				"Ne":          JSONNe[kindRecord, string],
				"NotIn":       JSONNotIn[kindRecord, string],
				"Set":         JSONSet[kindRecord, string],
			},
		},
		{
			typ: reflect.TypeFor[NullableJSON[kindRecord, string]](),
			ops: map[string]any{
				"Asc":         NullableJSONAsc[kindRecord, string],
				"ContainedBy": NullableJSONContainedBy[kindRecord, string],
				"Contains":    NullableJSONContains[kindRecord, string],
				"Desc":        NullableJSONDesc[kindRecord, string],
				"Eq":          NullableJSONEq[kindRecord, string],
				"In":          NullableJSONIn[kindRecord, string],
				"IsJSONNull":  NullableJSONIsJSONNull[kindRecord, string],
				"IsNotNull":   NullableJSONIsNotNull[kindRecord, string],
				"IsNull":      NullableJSONIsNull[kindRecord, string],
				"Ne":          NullableJSONNe[kindRecord, string],
				"NotIn":       NullableJSONNotIn[kindRecord, string],
				"Set":         NullableJSONSet[kindRecord, string],
				"SetNull":     NullableJSONSetNull[kindRecord, string],
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.typ.Name(), func(t *testing.T) {
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
