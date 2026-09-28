package value_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type preferences struct {
	Name    string                 `json:"name"`
	Count   int64                  `json:"count"`
	Tags    []string               `json:"tags"`
	Labels  map[string]string      `json:"labels"`
	Note    value.Nullable[string] `json:"note"`
	Extra   value.Optional[int]    `json:"extra,omitzero"`
	Enabled bool                   `json:"enabled,omitempty"`
}

func TestTypedJSONOwnsCanonicalSnapshot(t *testing.T) {
	source := preferences{Name: "go", Count: 9007199254740993, Tags: []string{"a", "b"}, Labels: map[string]string{"z": "last", "a": "first"}}
	doc, err := value.NewJSON(source)
	if err != nil {
		t.Fatal(err)
	}
	source.Tags[0] = "changed"
	source.Labels["a"] = "changed"
	first, err := doc.Decode()
	if err != nil || first.Tags[0] != "a" || first.Labels["a"] != "first" || first.Count != 9007199254740993 {
		t.Fatal(first, err)
	}
	first.Tags[0] = "again"
	first.Labels["a"] = "again"
	second, err := doc.Decode()
	if err != nil || second.Tags[0] != "a" || second.Labels["a"] != "first" {
		t.Fatal(second, err)
	}
	same, err := value.ParseJSON[preferences](`{"count":9007199254740993.0,"labels":{"z":"last","a":"first"},"tags":["a","b"],"note":null,"name":"go"}`)
	if err != nil || same != doc {
		t.Fatal("JSON canonical equality", err)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var restored value.JSON[preferences]
	if err := json.Unmarshal(encoded, &restored); err != nil || restored != doc {
		t.Fatal(err)
	}
}

func TestTypedJSONNullAndMissing(t *testing.T) {
	if _, err := value.NewJSON(value.JSON[string]{}); err == nil {
		t.Fatal("invalid nested document encoded")
	}
	if _, err := (value.JSON[string]{}).Decode(); err == nil {
		t.Fatal("zero document decoded")
	}
	null, err := value.NewJSON(value.Null[preferences]())
	if err != nil || !null.IsJSONNull() || null.IsZero() {
		t.Fatal(null, err)
	}
	if v, err := null.Decode(); err != nil || !v.IsNull() {
		t.Fatal(v, err)
	}
	if encoded, err := json.Marshal(value.Of(null)); err != nil || string(encoded) != "null" {
		t.Fatal(string(encoded), err)
	}
	var optional value.Optional[value.JSON[value.Nullable[preferences]]]
	if err := json.Unmarshal([]byte("null"), &optional); err != nil || !optional.IsSet() {
		t.Fatal(err)
	}
	for _, text := range []string{
		`null`, `{}`, `{"Name":"go","count":1,"tags":[],"labels":{},"note":null}`,
		`{"name":null,"count":1,"tags":[],"labels":{},"note":null}`,
		`{"name":"go","count":null,"tags":[],"labels":{},"note":null}`,
		`{"name":"go","count":1,"tags":[],"labels":{},"note":null,"unknown":0}`,
		`{"name":"go","count":1,"tags":[],"labels":{},"note":null,"extra":null}`,
		`{"name":"go","count":1,"tags":[],"labels":{},"note":null,"name":"other"}`,
	} {
		if _, err := value.ParseJSON[preferences](text); err == nil {
			t.Fatal("invalid shape accepted", text)
		}
	}
	current, _ := value.NewJSON("preserved")
	if err := json.Unmarshal([]byte(`42`), &current); err == nil {
		t.Fatal("wrong JSON type decoded")
	}
	if got, _ := current.Decode(); got != "preserved" {
		t.Fatal(got)
	}
}

func TestJSONExactNumbersUnicodeAndBounds(t *testing.T) {
	for _, text := range []string{`{"n":1e2}`, `{"n":100.00}`, `{"\u006e":100}`} {
		v, err := value.ParseJSON[map[string]json.Number](text)
		if err != nil {
			t.Fatal(err)
		}
		canonical, _ := v.Text()
		if canonical != `{"n":100}` {
			t.Fatal(canonical)
		}
	}
	for _, text := range []string{`"\ud800"`, `"\udc00"`, `"\ud800\u0041"`, `"\u0000"`, `{"a":1,"\u0061":2}`, `1e99999999`, `[1] true`, string([]byte{'"', 0xff, '"'}), strings.Repeat("[", value.JSONMaxDepth+1) + "0" + strings.Repeat("]", value.JSONMaxDepth+1), `"` + strings.Repeat("x", value.JSONMaxBytes) + `"`} {
		if _, err := value.ParseJSON[any](text); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	emoji, err := value.ParseJSON[string](`"\ud83d\ude00"`)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := emoji.Decode(); got != "😀" {
		t.Fatal(got)
	}
	dynamic, err := value.ParseJSON[any](`{"exact":9007199254740993}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := dynamic.Decode()
	if err != nil || !reflect.DeepEqual(got, map[string]any{"exact": json.Number("9007199254740993")}) {
		t.Fatal(got, err)
	}
}

type profileBase struct {
	Title string `json:"title"`
}
type promoted struct {
	profileBase
	Created time.Time `json:"created"`
	Number  int       `json:"number,string"`
}

func TestJSONPromotionAndCustomScalar(t *testing.T) {
	original := promoted{profileBase: profileBase{Title: "typed"}, Created: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Number: 42}
	doc, err := value.NewJSON(original)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := doc.Decode(); err != nil || got != original {
		t.Fatal(got, err)
	}
}

func TestJSONQuotedFieldsCannotBypassShape(t *testing.T) {
	type invalid struct {
		Nested struct{ Name string } `json:"nested,string"`
	}
	if _, err := value.ParseJSON[invalid](`{"nested":{"Name":null}}`); err == nil {
		t.Fatal("composite string tag bypassed shape validation")
	}
	type quoted struct {
		Number *int `json:"number,string"`
	}
	if _, err := value.ParseJSON[quoted](`{"number":null}`); err != nil {
		t.Fatal(err)
	}
	if _, err := value.ParseJSON[quoted](`{"number":"42"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := value.ParseJSON[quoted](`{"number":42}`); err == nil {
		t.Fatal("unquoted value accepted")
	}
	if _, err := value.ParseJSON[quoted](`{"number":"null"}`); err == nil {
		t.Fatal("quoted null accepted")
	}
}

func TestNewJSONRejectsLossyOrUnboundedInput(t *testing.T) {
	bad := string([]byte{0xff})
	for _, input := range []any{bad, map[string]string{bad: "value"}, value.Of(bad), value.Set(bad), []string{bad}, strings.Repeat("x", value.JSONMaxBytes+1), make([]int, value.JSONMaxNodes+1)} {
		if _, err := value.NewJSON(input); err == nil {
			t.Fatal("lossy or unbounded input accepted")
		}
	}
	type node struct{ Next *node }
	cycle := &node{}
	cycle.Next = cycle
	if _, err := value.NewJSON(cycle); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestJSONNumberExpansionAndNodesAreBounded(t *testing.T) {
	for _, text := range []string{
		"[" + strings.Repeat("1e4094,", 1000) + "0]",
		"[" + strings.Repeat("0,", value.JSONMaxNodes) + "0]",
	} {
		if _, err := value.ParseJSON[any](text); err == nil {
			t.Fatal("expanded document exceeded bounds")
		}
	}
}

func TestJSONCustomTextAndTypedRanges(t *testing.T) {
	type limits struct {
		Price decimal.Decimal `json:"price"`
		Small int8            `json:"small"`
		Fixed [2]int          `json:"fixed"`
	}
	price, err := decimal.Parse("9007199254740993.123456789")
	if err != nil {
		t.Fatal(err)
	}
	want := limits{Price: price, Small: 127, Fixed: [2]int{1, 2}}
	doc, err := value.NewJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := doc.Decode(); err != nil || got != want {
		t.Fatal(got, err)
	}
	for _, text := range []string{
		`{"price":1.25,"small":1,"fixed":[1,2]}`,
		`{"price":"1.25","small":128,"fixed":[1,2]}`,
		`{"price":"1.25","small":1,"fixed":[1]}`,
		`{"price":"1.25","small":1,"fixed":[1,2,3]}`,
		`{"price":"1.25","small":1,"fixed":[1,null]}`,
	} {
		if _, err := value.ParseJSON[limits](text); err == nil {
			t.Fatal("invalid concrete JSON value accepted", text)
		}
	}
}

func pointerJSONRoundTrip[T any](t *testing.T, input *T) {
	t.Helper()
	doc, err := value.NewJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := doc.Decode()
	if err != nil || !reflect.DeepEqual(decoded, input) {
		t.Fatal("pointer payload changed", err)
	}
	if input != nil && decoded == input {
		t.Fatal("decoded pointer aliases input")
	}
}

func TestJSONPointerWrappers(t *testing.T) {
	optional, nullable := value.Set("go"), value.Of("go")
	inner, err := value.NewJSON("go")
	if err != nil {
		t.Fatal(err)
	}
	pointerJSONRoundTrip(t, &optional)
	pointerJSONRoundTrip(t, &nullable)
	pointerJSONRoundTrip(t, &inner)
	pointerJSONRoundTrip(t, (*value.Optional[string])(nil))
	pointerJSONRoundTrip(t, (*value.Nullable[string])(nil))
	pointerJSONRoundTrip(t, (*value.JSON[string])(nil))
	type nested struct {
		Child any `json:"child"`
	}
	if _, err := value.NewJSON(nested{Child: (*value.Optional[string])(nil)}); err != nil {
		t.Fatal(err)
	}
	type typed struct {
		Child *value.Nullable[string] `json:"child"`
	}
	if _, err := value.ParseJSON[typed](`{"child":"go"}`); err != nil {
		t.Fatal(err)
	}
}

func TestJSONConcurrentShapeAndDecode(t *testing.T) {
	type payload struct {
		Labels map[string]string `json:"labels"`
	}
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			for range 8 {
				doc, err := value.ParseJSON[payload](`{"labels":{"a":"original"}}`)
				if err != nil {
					t.Error(err)
					return
				}
				first, err := doc.Decode()
				if err != nil {
					t.Error(err)
					return
				}
				first.Labels["a"] = "changed"
				second, err := doc.Decode()
				if err != nil || second.Labels["a"] != "original" {
					t.Error("decoded maps share state", err)
					return
				}
			}
		})
	}
	group.Wait()
}

type foldedJSONKey string

func (k *foldedJSONKey) UnmarshalText(data []byte) error {
	*k = foldedJSONKey(strings.ToLower(string(data)))
	return nil
}

func TestJSONTypedMapKeysDoNotLoseIdentity(t *testing.T) {
	for _, text := range []string{`{"01":"a","1":"b"}`, `{"01":"a"}`, `{"+1":"a"}`, `{"-0":"a"}`, `{"128":"a"}`} {
		if _, err := value.ParseJSON[map[int8]string](text); err == nil {
			t.Fatal("noncanonical or out-of-range map key accepted", text)
		}
	}
	if _, err := value.ParseJSON[map[uint8]string](`{"-1":"a"}`); err == nil {
		t.Fatal("negative unsigned map key accepted")
	}
	if _, err := value.ParseJSON[map[foldedJSONKey]string](`{"GO":"a","go":"b"}`); err == nil {
		t.Fatal("custom key collision accepted")
	}
	doc, err := value.NewJSON(map[int64]string{-1: "first", 9007199254740993: "exact"})
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := doc.Decode(); err != nil || decoded[9007199254740993] != "exact" || decoded[-1] != "first" {
		t.Fatal(decoded, err)
	}
	if _, err := value.NewJSON(map[foldedJSONKey]string{"go": "valid"}); err != nil {
		t.Fatal(err)
	}
}

func FuzzTypedJSONCanonical(f *testing.F) {
	for _, text := range []string{`null`, `{"z":1e2,"a":[true,"😀"]}`, `{"number":9007199254740993}`, `[]`} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		first, err := value.ParseJSON[any](text)
		if err != nil {
			return
		}
		canonical, err := first.Text()
		if err != nil {
			t.Fatal(err)
		}
		second, err := value.ParseJSON[any](canonical)
		if err != nil || first != second {
			t.Fatal("unstable canonical JSON", err)
		}
	})
}
