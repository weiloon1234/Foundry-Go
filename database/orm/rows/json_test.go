package rows

import (
	"database/sql/driver"
	"errors"
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
	src := FromModel(d.Query().Limit(0))
	field := expr.JSONFieldOf(func(p *jsonTestPayload) *value.Optional[string] { return &p.Name })
	foreign := expr.JSONOptional(expr.JSONProperty(expr.JSONRoot(expr.Value(f.Doc)), field))
	rejectScalar(t, src, expr.JSONTextScalar(foreign, codec.String[string]()).Value(), codec.Nullable(codec.String[string]()))
	bad := expr.At(ModelScope(d.Query()), f.Doc)
	path := expr.JSONProperty(expr.JSONRoot(bad), field)
	rejectScalar(t, src, path.JSON().Param(value.Null[value.JSON[value.Optional[string]]]()).Value(), codec.Nullable(codec.JSON[value.Optional[string]]()))
	root := expr.JSONRoot(expr.Value(d.Doc))
	zero := expr.JSONProperty(root, expr.JSONField[jsonTestPayload, string]{})
	rejectScalar(t, src, zero.JSON().Value(), codec.Nullable(codec.JSON[string]()))
	rejectScalar(t, src, expr.JSONIndex(expr.JSONRoot(expr.Value(d.Bytes)), 0).JSON().Value(), codec.Nullable(codec.JSON[byte]()))
	keys := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *map[int]string { return &p.Keys }))
	rejectScalar(t, src, expr.JSONScalar(keys, codec.Codec[map[int]string]{}).Value(), codec.Nullable(codec.Codec[map[int]string]{}))
	labels := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *map[string]string { return &p.Labels }))
	for _, key := range []string{"bad\x00key", string([]byte{0xff})} {
		rejectScalar(t, src, expr.JSONMapKey(labels, key).JSON().Value(), codec.Nullable(codec.JSON[string]()))
	}

	state := &driverState{}
	_, err := expressionQuery(src, expr.Count[jsonTestRecord]().Value(), codec.Signed[int64]()).Where(foreign.Contains("secret")).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
	// Zero paths and invalid enum values must also fail before a LIMIT 0 short circuit.
	rejectScalar(t, src, expr.JSONPath[jsonTestRecord, string]{}.Kind().Value(), codec.Nullable(codec.String[expr.JSONKind]()))
	rejectScalar(t, src, expr.JSONOptional(expr.JSONPath[jsonTestRecord, value.Optional[string]]{}).JSON().Param(value.Null[value.JSON[string]]()).Value(), codec.Nullable(codec.JSON[string]()))
	kind := expr.JSONType(expr.Value(d.Doc))
	state = &driverState{}
	_, err = expressionQuery(src, kind.Value(), codec.String[expr.JSONKind]()).Where(kind.Eq(expr.JSONKind("bad"))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}

func TestJSONPathSQLAndNullDecoding(t *testing.T) {
	d := jsonTestDeclare()
	src := FromModel(d.Query())
	root := expr.JSONRoot(expr.Value(d.Doc))
	name := expr.JSONOptional(expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *value.Optional[string] { return &p.Name })))
	age := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *int64 { return &p.Age }))
	keys := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *map[int]string { return &p.Keys }))
	scalar := expr.JSONOrderedScalar(age, codec.Signed[int64]())
	s := expressionSQL(t, expressionQuery(src, scalar.Value(), codec.Nullable(codec.Signed[int64]())).Where(expr.JSONTextScalar(expr.JSONMapKey(keys, 2), codec.String[string]()).Like("a%"), name.Exists(), name.IsMissing(), name.IsJSONNull()))
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
		got, err := expressionQuery(src, name.Kind().Value(), codec.Nullable(codec.String[expr.JSONKind]())).All(t.Context(), openTest(t, state))
		if err != nil || len(got) != 1 {
			t.Fatal(got, err)
		}
		if sample == nil && !got[0].Value.IsNull() {
			t.Fatal(got)
		}
		if sample == "null" {
			v, ok := got[0].Value.Get()
			if !ok || v != expr.JSONNull {
				t.Fatal(got)
			}
		}
	}
	// Invalid JSON scalar shape is rejected, not silently coerced by the result codec.
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"null"}}}
	if _, err := expressionQuery(src, name.JSON().Value(), codec.Nullable(codec.JSON[string]())).All(t.Context(), openTest(t, state)); err == nil {
		t.Fatal("JSON null decoded as required string")
	}
}

func TestJSONContainsSnapshotsFragment(t *testing.T) {
	d := jsonTestDeclare()
	src := FromModel(d.Query())
	root := expr.JSONRoot(expr.Value(d.Doc))
	values := expr.JSONProperty(root, expr.JSONFieldOf(func(p *jsonTestPayload) *[]int64 { return &p.Values }))
	fragment := []int64{1, 2}
	p := values.Contains(fragment)
	fragment[0] = 99
	s := expressionSQL(t, expressionQuery(src, values.Length().Value(), codec.Nullable(codec.Signed[int64]())).Where(p))
	if !reflect.DeepEqual(s.Bindings, []any{"[1,2]", true}) {
		t.Fatal(s.Bindings)
	}
}
