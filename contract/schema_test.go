package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/value"
)

func profileSchema() Schema {
	return Schema{Root: "Profile", Types: []Type{
		{ID: "Profile", Kind: ObjectKind, Properties: []Property{
			{Name: "name", Type: "text", Required: true},
			{Name: "age", Type: "age"},
			{Name: "status", Type: "status", Required: true},
			{Name: "nickname", Type: "nullable_text"},
			{Name: "friends", Type: "friends"},
			{Name: "scores", Type: "scores"},
			{Name: "quoted", Type: "quoted"},
		}},
		{ID: "text", Kind: StringKind},
		{ID: "nullable_text", Kind: AliasKind, Element: "text", Nullable: true},
		{ID: "age", Kind: IntegerKind, Bits: 8},
		{ID: "status", Kind: StringKind, Cases: []json.RawMessage{json.RawMessage(`"enabled"`), json.RawMessage(`"disabled"`)}},
		{ID: "friends", Kind: ArrayKind, Element: "Profile", Nullable: true},
		{ID: "scores", Kind: MapKind, Element: "age"},
		{ID: "quoted", Kind: QuotedKind, Element: "age"},
	}}
}

func compileForTest(t *testing.T, schema Schema) *compiledSchema {
	t.Helper()
	compiled, err := compileSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func checkForTest(t *testing.T, schema *compiledSchema, data string, limits shapeLimits) ([]Issue, error) {
	t.Helper()
	node, err := jsonwire.Decode([]byte(data), jsonwire.Limits{Bytes: jsonwire.MaxBytes, Depth: jsonwire.MaxDepth, Nodes: jsonwire.MaxNodes})
	if err != nil {
		t.Fatal(err)
	}
	return schema.check(context.Background(), node, limits)
}

func TestSchemaOwnsNormalizedMetadata(t *testing.T) {
	input := profileSchema()
	compiled := compileForTest(t, input)
	before := compiled.snapshot()
	input.Types[0].Properties[0].Name = "mutated"
	input.Types[4].Cases[0][1] = 'X'
	snapshot := compiled.snapshot()
	for i := range snapshot.Types {
		if len(snapshot.Types[i].Properties) > 0 {
			snapshot.Types[i].Properties[0].Name = "also_mutated"
		}
		if len(snapshot.Types[i].Cases) > 0 {
			snapshot.Types[i].Cases[0][1] = 'Y'
		}
	}
	if !reflect.DeepEqual(before, compiled.snapshot()) {
		t.Fatal("caller changed owned metadata")
	}
	for i := 1; i < len(before.Types); i++ {
		if before.Types[i-1].ID >= before.Types[i].ID {
			t.Fatal("types are not normalized")
		}
	}
	issues, err := checkForTest(t, compiled, `{"name":"N","status":"enabled"}`, shapeLimits{steps: 100, issues: 10})
	if err != nil || len(issues) != 0 {
		t.Fatalf("owned validation changed: %v %v", issues, err)
	}
}

func TestSchemaRejectsInvalidDeclarations(t *testing.T) {
	tests := map[string]Schema{
		"empty":                        {},
		"missing_root":                 {Root: "missing", Types: []Type{{ID: "text", Kind: StringKind}}},
		"duplicate_type":               {Root: "a", Types: []Type{{ID: "a", Kind: StringKind}, {ID: "a", Kind: IntegerKind}}},
		"duplicate_field":              {Root: "a", Types: []Type{{ID: "a", Kind: ObjectKind, Properties: []Property{{Name: "x", Type: "a"}, {Name: "x", Type: "a"}}}}},
		"missing_field_type":           {Root: "a", Types: []Type{{ID: "a", Kind: ObjectKind, Properties: []Property{{Name: "x", Type: "missing"}}}}},
		"missing_element":              {Root: "a", Types: []Type{{ID: "a", Kind: ArrayKind, Element: "missing"}}},
		"alias_cycle":                  {Root: "a", Types: []Type{{ID: "a", Kind: AliasKind, Element: "b"}, {ID: "b", Kind: AliasKind, Element: "a"}}},
		"quoted_cycle":                 {Root: "a", Types: []Type{{ID: "a", Kind: QuotedKind, Element: "a"}}},
		"quoted_object":                {Root: "a", Types: []Type{{ID: "a", Kind: QuotedKind, Element: "b"}, {ID: "b", Kind: ObjectKind}}},
		"quoted_quoted":                {Root: "a", Types: []Type{{ID: "a", Kind: QuotedKind, Element: "b"}, {ID: "b", Kind: QuotedKind, Element: "c"}, {ID: "c", Kind: StringKind}}},
		"wrong_bits":                   {Root: "a", Types: []Type{{ID: "a", Kind: IntegerKind, Bits: 12}}},
		"unknown_format":               {Root: "a", Types: []Type{{ID: "a", Kind: StringKind, Format: "guess"}}},
		"misplaced_format":             {Root: "a", Types: []Type{{ID: "a", Kind: IntegerKind, Format: UUIDFormat}}},
		"misplaced_property":           {Root: "a", Types: []Type{{ID: "a", Kind: StringKind, Properties: []Property{{Name: "x", Type: "a"}}}}},
		"misplaced_length":             {Root: "a", Types: []Type{{ID: "a", Kind: StringKind, Length: value.Set(0)}}},
		"negative_length":              {Root: "a", Types: []Type{{ID: "a", Kind: ArrayKind, Element: "a", Length: value.Set(-1)}}},
		"enum_type":                    {Root: "a", Types: []Type{{ID: "a", Kind: StringKind, Cases: []json.RawMessage{json.RawMessage(`1`)}}}},
		"enum_overflow":                {Root: "a", Types: []Type{{ID: "a", Kind: IntegerKind, Bits: 8, Cases: []json.RawMessage{json.RawMessage(`256`)}}}},
		"enum_duplicate":               {Root: "a", Types: []Type{{ID: "a", Kind: StringKind, Cases: []json.RawMessage{json.RawMessage(`"x"`), json.RawMessage(`"\u0078"`)}}}},
		"enum_negative_zero_duplicate": {Root: "a", Types: []Type{{ID: "a", Kind: IntegerKind, Signed: true, Cases: []json.RawMessage{json.RawMessage(`0`), json.RawMessage(`-0`)}}}},
		"name_bound":                   {Root: "a", Types: []Type{{ID: "a", Kind: ObjectKind, Properties: []Property{{Name: strings.Repeat("x", jsonwire.MaxBytes), Type: "a"}}}}},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			compiled, err := compileSchema(input)
			if compiled != nil || !errors.Is(err, fault.Invalid) {
				t.Fatalf("invalid declaration accepted: %v", err)
			}
		})
	}
}

func TestSchemaStrictFieldsAndValues(t *testing.T) {
	schema := compileForTest(t, profileSchema())
	tests := []struct {
		name, data string
		want       []Issue
	}{
		{"valid", `{"name":"N","status":"enabled","age":255,"nickname":null,"friends":null,"quoted":"0"}`, nil},
		{"nul_text", `{"name":"\u0000","status":"enabled"}`, nil},
		{"recursive", `{"name":"N","status":"enabled","friends":[{"name":"M","status":"disabled"}]}`, nil},
		{"required", `{}`, []Issue{{Path: "/name", Code: RequiredIssue}, {Path: "/status", Code: RequiredIssue}}},
		{"case_sensitive", `{"Name":"N","status":"enabled"}`, []Issue{{Path: "", Code: UnknownIssue}, {Path: "/name", Code: RequiredIssue}}},
		{"empty_unknown", `{"name":"N","status":"enabled","":1}`, []Issue{{Path: "", Code: UnknownIssue}}},
		{"redacted_unknown", `{"name":"N","status":"enabled","secret value":1,"another secret":2}`, []Issue{{Path: "", Code: UnknownIssue}}},
		{"null", `{"name":null,"status":"enabled"}`, []Issue{{Path: "/name", Code: NullIssue}}},
		{"wrong_type", `{"name":1,"status":"enabled"}`, []Issue{{Path: "/name", Code: TypeIssue}}},
		{"enum", `{"name":"N","status":"ENABLED"}`, []Issue{{Path: "/status", Code: ValueIssue}}},
		{"overflow", `{"name":"N","status":"enabled","age":256}`, []Issue{{Path: "/age", Code: ValueIssue}}},
		{"fraction", `{"name":"N","status":"enabled","age":1.0}`, []Issue{{Path: "/age", Code: ValueIssue}}},
		{"quoted_native_number", `{"name":"N","status":"enabled","quoted":1}`, []Issue{{Path: "/quoted", Code: TypeIssue}}},
		{"quoted_null", `{"name":"N","status":"enabled","quoted":"null"}`, []Issue{{Path: "/quoted", Code: ValueIssue}}},
		{"quoted_overflow", `{"name":"N","status":"enabled","quoted":"256"}`, []Issue{{Path: "/quoted", Code: ValueIssue}}},
		{"array_path", `{"name":"N","status":"enabled","friends":[{"name":null}]}`, []Issue{{Path: "/friends/0/name", Code: NullIssue}, {Path: "/friends/0/status", Code: RequiredIssue}}},
		{"map_paths", `{"name":"N","status":"enabled","scores":{"z":-1,"a~/":256}}`, []Issue{{Path: "/scores/a~0~1", Code: ValueIssue}, {Path: "/scores/z", Code: ValueIssue}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for range 5 {
				got, err := checkForTest(t, schema, test.data, shapeLimits{steps: 1000, issues: 20})
				if err != nil || !reflect.DeepEqual(got, test.want) {
					t.Fatalf("got %v, %v; want %v", got, err, test.want)
				}
			}
		})
	}
}

func TestSchemaBoundsAndCancellation(t *testing.T) {
	schema := compileForTest(t, profileSchema())
	issues, err := checkForTest(t, schema, `{}`, shapeLimits{steps: 100, issues: 1})
	if err != nil || !reflect.DeepEqual(issues, []Issue{{Path: "/name", Code: RequiredIssue}}) {
		t.Fatalf("issue limit: %v %v", issues, err)
	}
	issues, err = checkForTest(t, schema, `{"name":"N","status":"enabled"}`, shapeLimits{steps: 1, issues: 20})
	if issues != nil || !errors.Is(err, fault.Invalid) {
		t.Fatalf("work limit: %v %v", issues, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	issues, err = schema.check(ctx, map[string]any{}, shapeLimits{steps: 100, issues: 20})
	if issues != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v %v", issues, err)
	}
	for _, limits := range []shapeLimits{{steps: 0, issues: 1}, {steps: 1, issues: 0}, {steps: -1, issues: 1}} {
		issues, err = schema.check(context.Background(), nil, limits)
		if issues != nil || !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid limits: %v %v", issues, err)
		}
	}
	// Alias depth is work, not stack depth. A large acyclic graph compiles; the
	// runtime bound rejects it without walking the remainder or returning issues.
	input := Schema{Root: "0"}
	for i := range 2000 {
		input.Types = append(input.Types, Type{ID: TypeID(fmt.Sprint(i)), Kind: AliasKind, Element: TypeID(fmt.Sprint(i + 1))})
	}
	input.Types = append(input.Types, Type{ID: "2000", Kind: StringKind})
	long := compileForTest(t, input)
	issues, err = long.check(context.Background(), "x", shapeLimits{steps: 100, issues: 1})
	if issues != nil || !errors.Is(err, fault.Invalid) {
		t.Fatalf("alias work bound: %v %v", issues, err)
	}
	issues, err = long.check(context.Background(), "x", shapeLimits{steps: 2001, issues: 1})
	if err != nil || len(issues) != 0 {
		t.Fatalf("legal alias chain: %v %v", issues, err)
	}
}

func TestSchemaFixedArraysAndIntegerEnumIdentity(t *testing.T) {
	schema := compileForTest(t, Schema{Root: "array", Types: []Type{
		{ID: "array", Kind: ArrayKind, Element: "item", Length: value.Set(0)},
		{ID: "item", Kind: IntegerKind, Signed: true, Cases: []json.RawMessage{json.RawMessage(`0`)}},
	}})
	issues, err := checkForTest(t, schema, `[]`, shapeLimits{steps: 10, issues: 10})
	if err != nil || len(issues) != 0 {
		t.Fatalf("fixed empty array: %v %v", issues, err)
	}
	issues, err = checkForTest(t, schema, `[0]`, shapeLimits{steps: 10, issues: 10})
	if err != nil || !reflect.DeepEqual(issues, []Issue{{Path: "", Code: LengthIssue}}) {
		t.Fatalf("fixed length: %v %v", issues, err)
	}
	input := schema.snapshot()
	input.Root = "item"
	integer := compileForTest(t, input)
	issues, err = checkForTest(t, integer, `-0`, shapeLimits{steps: 10, issues: 10})
	if err != nil || len(issues) != 0 {
		t.Fatalf("integer enum identity: %v %v", issues, err)
	}
}

func TestSchemaScalarRepresentations(t *testing.T) {
	tests := []struct {
		name      string
		typ       Type
		good, bad string
	}{
		{"uuid", Type{Kind: StringKind, Format: UUIDFormat}, `"0193fd8c-2075-7000-8000-000000000001"`, `"not-an-id"`},
		{"decimal", Type{Kind: StringKind, Format: DecimalFormat}, `"12345678901234567890.001"`, `12345678901234567890.001`},
		{"date", Type{Kind: StringKind, Format: DateFormat}, `"2024-02-29"`, `"2023-02-29"`},
		{"base64", Type{Kind: StringKind, Format: Base64Format}, `"YQ=="`, `"YR=="`},
		{"uint64", Type{Kind: IntegerKind, Bits: 64}, `18446744073709551615`, `18446744073709551616`},
		{"int64", Type{Kind: IntegerKind, Bits: 64, Signed: true}, `-9223372036854775808`, `-9223372036854775809`},
		{"float32", Type{Kind: NumberKind, Bits: 32}, `3.4e38`, `3.5e38`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.typ.ID = "scalar"
			schema := compileForTest(t, Schema{Root: "scalar", Types: []Type{test.typ}})
			issues, err := checkForTest(t, schema, test.good, shapeLimits{steps: 10, issues: 10})
			if err != nil || len(issues) != 0 {
				t.Fatalf("valid representation: %v %v", issues, err)
			}
			issues, err = checkForTest(t, schema, test.bad, shapeLimits{steps: 10, issues: 10})
			if err != nil || len(issues) != 1 {
				t.Fatalf("invalid representation: %v %v", issues, err)
			}
		})
	}
}

func FuzzSchemaBoundedValidation(f *testing.F) {
	schema, err := compileSchema(profileSchema())
	if err != nil {
		f.Fatal(err)
	}
	for _, data := range []string{`{}`, `{"name":"N","status":"enabled"}`, `{"friends":[{},null],"scores":{"~/":-1}}`, `null`, `{"quoted":"18446744073709551615"}`} {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data string) {
		node, err := jsonwire.Decode([]byte(data), jsonwire.Limits{Bytes: 4096, Depth: 16, Nodes: 500})
		if err != nil {
			return
		}
		limits := shapeLimits{steps: 1000, issues: 20}
		first, err1 := schema.check(context.Background(), node, limits)
		second, err2 := schema.check(context.Background(), node, limits)
		if len(first) > limits.issues || !reflect.DeepEqual(first, second) || fmt.Sprint(err1) != fmt.Sprint(err2) {
			t.Fatal("validation is unbounded or nondeterministic")
		}
	})
}

func TestSchemaNormalizesNativeWidthAndPreservesExactNumber(t *testing.T) {
	schema := compileForTest(t, Schema{Root: "native", Types: []Type{{ID: "native", Kind: IntegerKind, Signed: true}}})
	if schema.snapshot().Types[0].Bits != uint8(strconv.IntSize) {
		t.Fatal("native integer width was not made explicit for exporters")
	}
	exact := compileForTest(t, Schema{Root: "number", Types: []Type{{ID: "number", Kind: NumberKind}}})
	issues, err := checkForTest(t, exact, `1e10000`, shapeLimits{steps: 10, issues: 10})
	if err != nil || len(issues) != 0 {
		t.Fatalf("exact numeric lexeme was treated as floating point: %v %v", issues, err)
	}
}
