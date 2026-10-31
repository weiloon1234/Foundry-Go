package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import "github.com/weiloon1234/Foundry-Go/database"

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type jsonTestPayload struct {
	Name   value.Optional[string] `json:"name,omitzero"`
	Age    int64                  `json:"age,string"`
	Keys   map[int]string         `json:"keys"`
	Values []int64                `json:"values"`
	Labels map[string]string      `json:"labels"`
}
type jsonTestRecord struct {
	ID    int64
	Doc   value.JSON[jsonTestPayload]
	Maybe value.Nullable[value.JSON[jsonTestPayload]]
	Bytes value.JSON[[]byte]
}
type jsonTestTable struct {
	orm.Table[jsonTestRecord, int64]
	Doc   orm.JSON[jsonTestRecord, jsonTestPayload]
	Maybe orm.NullableJSON[jsonTestRecord, jsonTestPayload]
	Bytes orm.JSON[jsonTestRecord, []byte]
}

func jsonTestDeclare() jsonTestTable {
	return orm.Define("json_records", func(b orm.Builder[jsonTestRecord], r *jsonTestRecord) jsonTestTable {
		return jsonTestTable{orm.TableOf(b, &r.ID), orm.JSONOf(b, &r.Doc), orm.NullableJSONOf(b, &r.Maybe), orm.JSONOf(b, &r.Bytes)}
	})
}

func TestJSONPathOwnershipAndValidation(t *testing.T) {
	d, f := jsonTestDeclare(), jsonTestDeclare()
	src := rows.FromModel(d.Query().Limit(0))
	field := expr.JSONFieldOf(func(p *jsonTestPayload) *value.Optional[string] { return &p.Name })
	foreign := expr.JSONOptional(expr.JSONProperty(expr.JSONRoot(expr.Value(orm.JSONExpr(f.Doc))), field))
	rejectScalar(t, src, expr.AsSelected(expr.JSONTextScalar(foreign, codec.String[string]())), codec.Nullable(codec.String[string]()))
	bad := expr.At(rows.ModelScope(d.Query()), orm.JSONExpr(f.Doc))
	path := expr.JSONProperty(expr.JSONRoot(bad), field)
	rejectScalar(t, src, expr.AsSelected(expr.ParamOf(path.JSON(), value.Null[value.JSON[value.Optional[string]]]())), codec.Nullable(codec.JSON[value.Optional[string]]()))
	root := expr.JSONRoot(expr.Value(orm.JSONExpr(d.Doc)))
	zero := expr.JSONProperty(root, expr.JSONField[jsonTestPayload, string]{})
	rejectScalar(t, src, expr.AsSelected(zero.JSON()), codec.Nullable(codec.JSON[string]()))
	rejectScalar(t, src, expr.AsSelected(expr.JSONIndex(expr.JSONRoot(expr.Value(orm.JSONExpr(d.Bytes))), 0).JSON()), codec.Nullable(codec.JSON[byte]()))
	keys := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *map[int]string { return &p.Keys }))
	rejectScalar(t, src, expr.AsSelected(expr.JSONScalar(keys, codec.Codec[map[int]string]{})), codec.Nullable(codec.Codec[map[int]string]{}))
	labels := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *map[string]string { return &p.Labels }))
	for _, key := range []string{"bad\x00key", string([]byte{0xff})} {
		rejectScalar(t, src, expr.AsSelected(expr.JSONMapKey(labels, key).JSON()), codec.Nullable(codec.JSON[string]()))
	}

	document, captureErr := value.NewJSON("secret")
	if captureErr != nil {
		t.Fatal(captureErr)
	}
	state := &driverState{}
	_, err := rows.Read(expressionQuery(src, expr.AsSelected(expr.Count[jsonTestRecord]()), codec.Signed[int64]()).Where(expr.Eq(expr.JSONContainsNullable(foreign.JSON(), expr.ParamOf(foreign.JSON(), value.Of(document))), value.Of(true)))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
	// Zero paths and invalid enum values must also fail before a LIMIT 0 short circuit.
	rejectScalar(t, src, expr.AsSelected(expr.JSONTypeNullable(expr.JSONPath[jsonTestRecord, string]{}.JSON())), codec.Nullable(codec.String[database.JSONKind]()))
	rejectScalar(t, src, expr.AsSelected(expr.ParamOf(expr.JSONOptional(expr.JSONPath[jsonTestRecord, value.Optional[string]]{}).JSON(), value.Null[value.JSON[string]]())), codec.Nullable(codec.JSON[string]()))
	kind := expr.JSONType(expr.Value(orm.JSONExpr(d.Doc)))
	state = &driverState{}
	_, err = rows.Read(expressionQuery(src, expr.AsSelected(kind), codec.String[database.JSONKind]()).Where(expr.Eq(kind, database.JSONKind("bad")))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}

func TestJSONPathSQLAndNullDecoding(t *testing.T) {
	d := jsonTestDeclare()
	src := rows.FromModel(d.Query())
	root := expr.JSONRoot(expr.Value(orm.JSONExpr(d.Doc)))
	name := expr.JSONOptional(expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *value.Optional[string] { return &p.Name })))
	age := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *int64 { return &p.Age }))
	keys := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *map[int]string { return &p.Keys }))
	scalar := expr.OrderNullableRow(expr.JSONScalar(age, codec.Signed[int64]()))
	s := expressionSQL(t, expressionQuery(src, expr.AsSelected(scalar), codec.Nullable(codec.Signed[int64]())).Where(expr.NullableTextLike(expr.JSONTextScalar(expr.JSONMapKey(keys, 2), codec.String[string]()), "a%"), expr.IsNotNull(name.JSON()), expr.IsNull(name.JSON()), expr.Eq(expr.JSONTypeNullable(name.JSON()), value.Of(database.JSONNull))))
	for _, want := range []string{"E'age'", "AS jsonb", "E'name'", "IS NOT NULL", "IS NULL", "JSONB_TYPEOF"} {
		if !strings.Contains(s.SQLText, want) {
			t.Fatalf("missing %s in %s", want, s.SQLText)
		}
	}
	if !reflect.DeepEqual(s.Bindings, []any{"2", "a%", "null"}) {
		t.Fatal(s.Bindings)
	}
	// Present JSON null is distinct from SQL NULL when the declared payload can decode it.
	ptrField := expr.JSONFieldOf(func(p *struct {
		Nick *string `json:"nick"`
	}) **string {
		return &p.Nick
	})
	if err := ptrField.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []driver.Value{nil, "null"} {
		state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{sample}}}
		got, err := rows.Read(expressionQuery(src, expr.AsSelected(expr.JSONTypeNullable(name.JSON())), codec.Nullable(codec.String[database.JSONKind]()))).All(t.Context(), openTest(t, state))
		if err != nil || len(got) != 1 {
			t.Fatal(got, err)
		}
		if sample == nil && !got[0].Value.IsNull() {
			t.Fatal(got)
		}
		if sample == "null" {
			v, ok := got[0].Value.Get()
			if !ok || v != database.JSONNull {
				t.Fatal(got)
			}
		}
	}
	// Invalid JSON scalar shape is rejected, not silently coerced by the result codec.
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"null"}}}
	if _, err := rows.Read(expressionQuery(src, expr.AsSelected(name.JSON()), codec.Nullable(codec.JSON[string]()))).All(t.Context(), openTest(t, state)); err == nil {
		t.Fatal("JSON null decoded as required string")
	}
}

func TestJSONContainsSnapshotsFragment(t *testing.T) {
	d := jsonTestDeclare()
	src := rows.FromModel(d.Query())
	root := expr.JSONRoot(expr.Value(orm.JSONExpr(d.Doc)))
	values := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *[]int64 { return &p.Values }))
	fragment := []int64{1, 2}
	document, err := value.NewJSON(fragment)
	if err != nil {
		t.Fatal(err)
	}
	p := expr.Eq(expr.JSONContainsNullable(values.JSON(), expr.ParamOf(values.JSON(), value.Of(document))), value.Of(true))
	fragment[0] = 99
	s := expressionSQL(t, expressionQuery(src, expr.AsSelected(expr.JSONLengthNullable(values.JSON())), codec.Nullable(codec.Signed[int64]())).Where(p))
	if !reflect.DeepEqual(s.Bindings, []any{"[1,2]", true}) {
		t.Fatal(s.Bindings)
	}
}

// A fragment is captured once by the shared JSON owner before expression binding.
type rejectedJSONFragment struct{ cause error }

func (v rejectedJSONFragment) MarshalJSON() ([]byte, error) { return nil, v.cause }

func TestJSONContainmentCaptureRejectsInvalidFragmentBeforeSQL(t *testing.T) {
	cause := errors.New("private containment fragment")
	document, err := value.NewJSON(rejectedJSONFragment{cause: cause})
	if !errors.Is(err, cause) || !errors.Is(err, fault.Invalid) || !document.IsZero() {
		t.Fatal("fragment capture lost its cause, classification or zero result")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if strings.Contains(fmt.Sprintf(format, err), "private containment fragment") {
			t.Fatal("fragment capture disclosed an application error")
		}
	}
	// Even a caller that ignores a capture failure cannot bind its zero snapshot.
	type model struct {
		ID  int64
		Doc value.JSON[rejectedJSONFragment]
	}
	type fields struct {
		orm.Table[model, int64]
		Doc orm.JSON[model, rejectedJSONFragment]
	}
	d := orm.Define("fragment_records", func(b orm.Builder[model], m *model) fields {
		return fields{orm.TableOf(b, &m.ID), orm.JSONOf(b, &m.Doc)}
	})
	if err := orm.Check(d.Table); err != nil {
		t.Fatal("fragment test declaration must be valid", err)
	}
	path := expr.JSONRoot(expr.Value(orm.JSONExpr(d.Doc)))
	q := rows.FromModel(d.Query().Limit(0)).Where(expr.Eq(expr.JSONContainsNullable(path.JSON(), expr.ParamOf(path.JSON(), value.Of(document))), value.Of(true)))
	state := &driverState{}
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("invalid fragment reached SQL", err, state.sql)
	}
}

func TestJSONPathParametersAndBoundaries(t *testing.T) {
	type payload struct {
		Odd []string            `json:"odd_%"`
		Map map[string][]string `json:"map"`
	}
	type model struct {
		ID  int64
		Doc value.JSON[payload]
	}
	type table struct {
		orm.Table[model, int64]
		Doc orm.JSON[model, payload]
	}
	d := orm.Define("records", func(b orm.Builder[model], m *model) table { return table{orm.TableOf(b, &m.ID), orm.JSONOf(b, &m.Doc)} })
	root := expr.JSONRoot(expr.Value(orm.JSONExpr(d.Doc)))
	property := expr.JSONProperty(root, expr.JSONFieldOf(func(p *payload) *[]string { return &p.Odd }))
	scalar := expr.JSONTextScalar(expr.JSONIndex(property, -1), codec.String[string]())
	// Select only the key so the path appears once in the predicate.
	qID := rows.WhereScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(d.Key()))))), expr.NullableTextLike(scalar, "%go%"))
	s, err := rows.CompileRead(rows.ReadScalar(qID))
	if err != nil || !strings.Contains(s.SQL(), `-> CAST(CAST(E'odd_%' AS text) AS text)`) || strings.Contains(s.SQL(), "%go%") || !strings.Contains(s.SQL(), " AS integer)") || !reflect.DeepEqual(s.Arguments(), []any{int64(-1), "%go%"}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	keyed := expr.JSONMapKey(expr.JSONProperty(root, expr.JSONFieldOf(func(p *payload) *map[string][]string { return &p.Map })), "runtime'key")
	s, err = rows.CompileRead(rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(d.Key()))))), expr.IsNotNull(keyed.JSON()))))
	if err != nil || strings.Contains(s.SQL(), "runtime") || !reflect.DeepEqual(s.Arguments(), []any{"runtime'key"}) {
		t.Fatal("runtime key not bound", s.SQL(), s.Arguments(), err)
	}
}
func TestJSONOperationsBindAndValidate(t *testing.T) {
	d := jsonTestDeclare()
	document, e := value.NewJSON(jsonTestPayload{Labels: map[string]string{"x": "safe'_%"}})
	if e != nil {
		t.Fatal(e)
	}
	q := expr.Where(d.Query(), expr.Eq(expr.JSONContains(expr.Value(orm.JSONExpr(d.Doc)), expr.Param(orm.JSONExpr(d.Doc), document)), true))
	s, e := orm.Compile(q)
	want, e2 := codec.JSON[jsonTestPayload]().Bind(document)
	if e != nil || e2 != nil || !strings.Contains(s.SQL(), " @> ") || strings.Contains(s.SQL(), "safe") || !reflect.DeepEqual(s.Arguments(), []any{want, true}) {
		t.Fatal(s.SQL(), s.Arguments(), e, e2)
	}
	source := rows.From(d.Query().Limit(0))
	badRepresentation := expr.Constant[jsonTestRecord](codec.JSON[jsonTestPayload]().WithParameterType(codec.TypeText), document)
	for _, x := range []expr.Selected[jsonTestRecord, database.JSONKind]{expr.AsSelected(expr.JSONType(badRepresentation)), expr.AsSelected(expr.JSONType[jsonTestRecord, value.JSON[jsonTestPayload]](nil))} {
		if _, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, x))); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid JSON type", e)
		}
	}
	if _, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, expr.AsSelected(expr.JSONContains(expr.Value(orm.JSONExpr(d.Doc)), expr.Param(orm.JSONExpr(d.Doc), value.JSON[jsonTestPayload]{})))))); !errors.Is(e, fault.Invalid) {
		t.Fatal("zero JSON bound", e)
	}
}
