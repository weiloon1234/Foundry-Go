package jsonwire

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func wireLimits() Limits { return Limits{Bytes: MaxBytes, Depth: MaxDepth, Nodes: MaxNodes} }

func TestTransportJSONPreservesTextAndNumberRepresentation(t *testing.T) {
	input := []byte(`{"\u0000key":"x\u0000y","amount":-0.1200e+100000,"large":9007199254740993}`)
	node, err := Decode(input, wireLimits())
	if err != nil {
		t.Fatal(err)
	}
	object, ok := node.(map[string]any)
	if !ok || object["\x00key"] != "x\x00y" || object["amount"] != json.Number("-0.1200e+100000") || object["large"] != json.Number("9007199254740993") {
		t.Fatalf("transport parser changed a value: %#v", node)
	}
	for i := range input {
		input[i] = ' '
	}
	if object["amount"] != json.Number("-0.1200e+100000") || object["\x00key"] != "x\x00y" {
		t.Fatal("decoded tree retained mutable input bytes")
	}
}

func TestDatabaseJSONKeepsItsCanonicalPolicy(t *testing.T) {
	canonical, node, err := Parse([]byte(`{"z":1.20e1,"a":-0.0}`))
	if err != nil || canonical != `{"a":0,"z":12}` {
		t.Fatalf("canonical storage JSON: %q %v", canonical, err)
	}
	if !reflect.DeepEqual(node, map[string]any{"a": json.Number("0"), "z": json.Number("12")}) {
		t.Fatalf("canonical tree: %#v", node)
	}
	for _, input := range []string{`"x\u0000y"`, `{"\u0000":true}`, `1e100000`} {
		if _, _, err := Parse([]byte(input)); !errors.Is(err, fault.Invalid) {
			t.Fatalf("storage policy accepted %q: %v", input, err)
		}
	}
}

func TestTransportJSONLimitsAreIndependentAndExact(t *testing.T) {
	for _, test := range []struct {
		name, input string
		limits      Limits
		valid       bool
	}{
		{"exact bytes", `"ab"`, Limits{Bytes: 4, Depth: 0, Nodes: 1}, true},
		{"too many bytes", `"ab"`, Limits{Bytes: 3, Depth: 0, Nodes: 1}, false},
		{"exact object nodes", `{"x":1}`, Limits{Bytes: 32, Depth: 1, Nodes: 3}, true},
		{"object key consumes node", `{"x":1}`, Limits{Bytes: 32, Depth: 1, Nodes: 2}, false},
		{"exact array nodes", `[1,2]`, Limits{Bytes: 32, Depth: 1, Nodes: 3}, true},
		{"array nodes exceeded", `[1,2]`, Limits{Bytes: 32, Depth: 1, Nodes: 2}, false},
		{"depth zero empty object", `{}`, Limits{Bytes: 32, Depth: 0, Nodes: 1}, true},
		{"depth exceeded", `[1]`, Limits{Bytes: 32, Depth: 0, Nodes: 3}, false},
		{"negative depth", `0`, Limits{Bytes: 32, Depth: -1, Nodes: 1}, false},
		{"recursion cap", `0`, Limits{Bytes: 32, Depth: MaxDepth + 1, Nodes: 1}, false},
		{"zero bytes", `0`, Limits{Nodes: 1}, false},
		{"zero nodes", `0`, Limits{Bytes: 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			node, err := Decode([]byte(test.input), test.limits)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
			if !test.valid && (node != nil || !errors.Is(err, fault.Invalid)) {
				t.Fatal("failed parse leaked a partial tree or lost classification")
			}
		})
	}
	large := []byte(`"` + strings.Repeat("x", MaxBytes) + `"`)
	if _, err := Decode(large, Limits{Bytes: len(large), Depth: 0, Nodes: 1}); err != nil {
		t.Fatalf("transport inherited storage byte limit: %v", err)
	}
	if _, _, err := Parse(large); err == nil {
		t.Fatal("storage byte limit changed")
	}
	array := []byte("[" + strings.Repeat("0,", MaxNodes) + "0]")
	if _, err := Decode(array, Limits{Bytes: len(array), Depth: 1, Nodes: MaxNodes + 2}); err != nil {
		t.Fatalf("transport inherited storage node limit: %v", err)
	}
	if _, _, err := Parse(array); err == nil {
		t.Fatal("storage node limit changed")
	}
}

func TestSharedJSONParserRejectsAmbiguousOrLossyInput(t *testing.T) {
	for _, input := range []string{
		`{"x":1,"\u0078":2}`, `{"outer":{"x":1,"x":2}}`,
		`"\ud800"`, `"\udc00"`, `"\ud800\u0000"`, `"\ud800x"`,
		`[1,]`, `{"x":}`, `01`, `1e`, `1e+`, `NaN`, `null false`, `"unterminated`,
		"\"\xff\"", "\"unescaped\x00\"", "",
	} {
		t.Run(input, func(t *testing.T) {
			if value, err := Decode([]byte(input), wireLimits()); value != nil || !errors.Is(err, fault.Invalid) {
				t.Fatalf("transport accepted malformed input: %#v %v", value, err)
			}
			if _, value, err := Parse([]byte(input)); value != nil || !errors.Is(err, fault.Invalid) {
				t.Fatalf("storage accepted malformed input: %#v %v", value, err)
			}
		})
	}
	input := []byte(`{"emoji":"\ud83d\ude00","literal":"\\u0000"}`)
	wire, err := Decode(input, wireLimits())
	if err != nil {
		t.Fatal(err)
	}
	_, stored, err := Parse(input)
	if err != nil || !reflect.DeepEqual(wire, stored) {
		t.Fatalf("valid escaped text differs: %v", err)
	}
}

func FuzzTransportJSONLosslessTree(f *testing.F) {
	for _, input := range []string{`{"x":1e30}`, `"\u0000"`, `[true,null,"\ud83d\ude00"]`, `{"x":1,"\u0078":2}`} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		node, err := Decode(data, Limits{Bytes: 4096, Depth: 16, Nodes: 512})
		if err != nil {
			return
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		var native any
		if err := decoder.Decode(&native); err != nil || !reflect.DeepEqual(node, native) {
			t.Fatalf("accepted JSON changed its native exact tree: %v", err)
		}
	})
}

// Measure is Decode's bound accounting without the tree, for encoder output:
// every document Decode accepts must measure identically, and bounds are named.
func TestMeasureMatchesDecodeBoundsWithoutBuildingATree(t *testing.T) {
	inputs := []string{
		`{"x":1}`, `[1,2]`, `{}`, `[]`, `"ab"`, `0`, `-1.5e+3`, `true`, `null`, `[1]`, `[[]]`, `[{}]`, `{"a":[]}`,
		`{"a":{"b":[1,{"c":"d"}]},"e":[true,false,null]}`, `{"q\"u":"a\\\"b","n":"x,y]z"}`,
		`{"emoji":"\ud83d\ude00"}`, "  [1, 2]  ", "{\"a\" : 1 , \"b\" :[ 1 , 2 ] }",
	}
	limits := []Limits{
		{Bytes: 4096, Depth: MaxDepth, Nodes: MaxNodes}, {Bytes: 32, Depth: 1, Nodes: 3}, {Bytes: 32, Depth: 0, Nodes: 1},
		{Bytes: 4, Depth: 0, Nodes: 1}, {Bytes: 64, Depth: 2, Nodes: 8}, {Bytes: 64, Depth: 3, Nodes: 5}, {Bytes: 128, Depth: 1, Nodes: 6},
	}
	for _, input := range inputs {
		if _, err := Decode([]byte(input), Limits{Bytes: 4096, Depth: MaxDepth, Nodes: MaxNodes}); err != nil {
			t.Fatalf("fixture %q is not valid JSON: %v", input, err)
		}
		for _, limit := range limits {
			_, decoded := Decode([]byte(input), limit)
			measured := Measure([]byte(input), limit)
			if (decoded == nil) != (measured == nil) {
				t.Fatalf("%q %+v: Decode=%v Measure=%v", input, limit, decoded, measured)
			}
			if measured != nil && !errors.Is(measured, fault.Invalid) {
				t.Fatalf("%q: Measure lost classification: %v", input, measured)
			}
		}
	}
	for _, test := range []struct {
		input   string
		limits  Limits
		message string
	}{
		{`"abc"`, Limits{Bytes: 3, Depth: 0, Nodes: 1}, "byte bound"},
		{`[[1]]`, Limits{Bytes: 32, Depth: 1, Nodes: 8}, "depth bound"},
		{`[1,2,3]`, Limits{Bytes: 32, Depth: 1, Nodes: 3}, "node bound"},
	} {
		for _, err := range []error{Measure([]byte(test.input), test.limits), func() error { _, err := Decode([]byte(test.input), test.limits); return err }()} {
			var failure *fault.Error
			if !errors.As(err, &failure) || !strings.Contains(failure.Message(), test.message) {
				t.Fatalf("%q: bound was not named: %v", test.input, err)
			}
		}
	}
	for _, input := range []string{"", "\"\xff\"", `"\ud800"`, `[`, `]`} {
		if Measure([]byte(input), Limits{Bytes: 64, Depth: 4, Nodes: 16}) == nil {
			t.Fatalf("Measure accepted %q", input)
		}
	}
}

func BenchmarkTransportDecodeAndCheck(b *testing.B) {
	var builder strings.Builder
	builder.WriteString(`{"items":[`)
	for i := range 100 {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`{"id":"item-1","name":"Example item","count":1234,"active":true}`)
	}
	builder.WriteString(`],"total":100}`)
	data := []byte(builder.String())
	limits := Limits{Bytes: len(data), Depth: 8, Nodes: 4096}
	b.Run("Decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Decode(data, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Measure", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := Measure(data, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
}
