package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type UnionFixtureValue struct{ stored UnionValue[UnionFixtureValue] }
type unionFixturePayload struct {
	Token string `json:"token"`
}

func (v UnionFixtureValue) MarshalJSON() ([]byte, error) { return v.stored.MarshalJSON() }
func (v *UnionFixtureValue) UnmarshalJSON(raw []byte) error {
	stored, err := DecodeUnion(unionFixtureJSON(), raw)
	if err != nil {
		return err
	}
	if _, err := UnionVariant[UnionFixtureValue, unionFixturePayload](stored, stored.Tag()); err != nil {
		return err
	}
	v.stored = stored
	return nil
}
func unionFixtureSchema() Schema {
	return Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.UnionFixtureValue", Types: []Type{
		{ID: "github.com/weiloon1234/Foundry-Go/contract.UnionFixtureValue", Kind: UnionKind, Discriminator: "kind", Variants: []Variant{{Tag: "card", Type: "payload"}, {Tag: "bank", Type: "payload"}}},
		{ID: "payload", Kind: ObjectKind, Properties: []Property{{Name: "token", Type: "text", Required: true}}},
		{ID: "text", Kind: StringKind},
	}}
}
func unionFixtureJSON() JSON[UnionFixtureValue] {
	return DefineJSONValue[UnionFixtureValue](unionFixtureSchema())
}

func TestUnionSchemaOwnershipAndRejections(t *testing.T) {
	d := unionFixtureJSON()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := d.Description()
	if err != nil {
		t.Fatal(err)
	}
	for i := range snapshot.Types {
		if snapshot.Types[i].Kind == UnionKind {
			snapshot.Types[i].Variants[0].Tag = "changed"
		}
	}
	original, _ := d.Description()
	for _, typ := range original.Types {
		for _, v := range typ.Variants {
			if v.Tag == "changed" {
				t.Fatal("shared variant snapshot")
			}
		}
	}
	cases := []func(*Schema){
		func(s *Schema) { s.Types[0].Discriminator = "" },
		func(s *Schema) { s.Types[0].Variants = nil },
		func(s *Schema) { s.Types[0].Variants[1].Tag = "card" },
		func(s *Schema) { s.Types[0].Variants[0].Type = "missing" },
		func(s *Schema) { s.Types[0].Variants[0].Type = "text" },
		func(s *Schema) { s.Types[1].Nullable = true },
		func(s *Schema) { s.Types[1].Properties[0].Name = "kind" },
		func(s *Schema) { s.Types[2].Variants = []Variant{{Tag: "card", Type: "payload"}} },
	}
	for i, change := range cases {
		s := unionFixtureSchema()
		change(&s)
		if _, err := s.Normalize(); err == nil {
			t.Fatalf("accepted invalid union %d", i)
		}
	}
}

func TestUnionWireRejectionsAndLimits(t *testing.T) {
	d := unionFixtureJSON()
	limits := JSONLimits{Bytes: 8192, Depth: 8, Nodes: 100, Steps: 500, Issues: 8}
	for raw, path := range map[string]string{
		`{}`: "/kind", `{"kind":7}`: "/kind", `{"kind":"future"}`: "/kind",
		`{"kind":"card"}`: "/token", `{"kind":"card","token":7}`: "/token", `{"kind":"card","token":"test","mixed":true}`: "",
	} {
		_, err := d.Decode(t.Context(), []byte(raw), limits)
		var decode *DecodeError
		if !errors.As(err, &decode) || len(decode.Issues()) == 0 || decode.Issues()[0].Path != path {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{"kind":"card","kind":"bank","token":"test"}`, `{"kind":"card","token":"\ud800"}`, `null`, `[]`, `{"kind":"card","token":"test"}false`} {
		if _, err := d.Decode(t.Context(), []byte(raw), limits); err == nil {
			t.Fatal("invalid wire accepted", raw)
		}
	}
	raw := []byte(`{"kind":"card","token":"test"}`)
	for _, tiny := range []JSONLimits{{Bytes: 4, Depth: 8, Nodes: 100, Steps: 100, Issues: 1}, {Bytes: 8192, Depth: 0, Nodes: 100, Steps: 100, Issues: 1}, {Bytes: 8192, Depth: 8, Nodes: 2, Steps: 100, Issues: 1}, {Bytes: 8192, Depth: 8, Nodes: 100, Steps: 1, Issues: 1}} {
		if _, err := d.Decode(t.Context(), raw, tiny); err == nil {
			t.Fatal("limit ignored")
		}
	}
	input, err := d.Decode(t.Context(), raw, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Encode(t.Context(), input, limits); err != nil {
		t.Fatal(err)
	}
	raw[0] = '['
	encoded, _ := input.MarshalJSON()
	if encoded[0] != '{' {
		t.Fatal("input bytes shared")
	}
	encoded[0] = '['
	again, _ := input.MarshalJSON()
	if again[0] != '{' {
		t.Fatal("output bytes shared")
	}
	if _, err = EncodeUnion(d, "future", unionFixturePayload{Token: "test"}); err == nil {
		t.Fatal("unknown constructor tag")
	}
	if _, err = EncodeUnion(d, "card", struct {
		Kind  string `json:"kind"`
		Token string `json:"token"`
	}{Kind: "card", Token: "test"}); err == nil {
		t.Fatal("duplicate constructor discriminator")
	}
}

func FuzzUnionJSON(f *testing.F) {
	for _, raw := range []string{`{"kind":"card","token":"test"}`, `{"kind":"future"}`, `{"kind":"bank","kind":"card"}`, `{"kind":null}`} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 8192 {
			return
		}
		limits := JSONLimits{Bytes: 8192, Depth: 8, Nodes: 200, Steps: 1000, Issues: 8}
		d := unionFixtureJSON()
		v, err := d.Decode(t.Context(), raw, limits)
		if err != nil {
			return
		}
		encoded, err := d.Encode(t.Context(), v, limits)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = d.Decode(t.Context(), encoded, limits); err != nil {
			t.Fatal(err)
		}
	})
}

func handwrittenUnion(raw []byte, tags map[string]bool) (UnionValue[UnionFixtureValue], error) {
	limits := unionLimits()
	node, err := jsonwire.Decode(raw, jsonwire.Limits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes})
	if err != nil {
		return UnionValue[UnionFixtureValue]{}, err
	}
	object, ok := node.(map[string]any)
	if !ok || len(object) != 2 {
		return UnionValue[UnionFixtureValue]{}, invalidUnion()
	}
	tag, ok := object["kind"].(string)
	if !ok || !tags[tag] {
		return UnionValue[UnionFixtureValue]{}, invalidUnion()
	}
	if _, ok := object["token"].(string); !ok {
		return UnionValue[UnionFixtureValue]{}, invalidUnion()
	}
	delete(object, "kind")
	payload, err := json.Marshal(object)
	if err != nil {
		return UnionValue[UnionFixtureValue]{}, err
	}
	return UnionValue[UnionFixtureValue]{wire: string(raw), payload: string(payload), tag: tag}, nil
}

func BenchmarkUnionDispatch(b *testing.B) {
	for _, size := range []int{2, 32} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			schema := unionFixtureSchema()
			schema.Types[0].Variants = nil
			tags := make(map[string]bool, size)
			for i := 0; i < size; i++ {
				tag := fmt.Sprint(i)
				tags[tag] = true
				schema.Types[0].Variants = append(schema.Types[0].Variants, Variant{Tag: tag, Type: "payload"})
			}
			d := DefineJSONValue[UnionFixtureValue](schema)
			raw := []byte(fmt.Sprintf(`{"kind":"%d","token":"test"}`, size-1))
			b.Run("contract", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := DecodeUnion(d, raw); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(raw)), "wire-B")
			})
			b.Run("handwritten", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := handwrittenUnion(raw, tags); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(raw)), "wire-B")
			})
		})
	}
}

func TestUnionDepthIsBoundedBeforeNativeDecode(t *testing.T) {
	raw := []byte(`{"kind":"card","token":` + strings.Repeat("[", MaxJSONDepth+1) + `0` + strings.Repeat("]", MaxJSONDepth+1) + `}`)
	if _, err := DecodeUnion(unionFixtureJSON(), raw); err == nil {
		t.Fatal("deep input accepted")
	}
	if !json.Valid([]byte(`{"kind":"card","token":"test"}`)) {
		t.Fatal("fixture invalid")
	}
}

type unionFailingScalar string

func (v *unionFailingScalar) UnmarshalJSON(raw []byte) error {
	if string(raw) == `"exit"` {
		runtime.Goexit()
	}
	panic("private union codec detail")
}

type unionFailingPayload struct {
	Token unionFailingScalar `json:"token"`
}
type UnionFailingValue struct{ stored UnionValue[UnionFailingValue] }

func (v UnionFailingValue) MarshalJSON() ([]byte, error) { return v.stored.MarshalJSON() }
func unionFailingJSON() JSON[UnionFailingValue] {
	s := unionFixtureSchema()
	s.Root = "github.com/weiloon1234/Foundry-Go/contract.UnionFailingValue"
	s.Types[0].ID = s.Root
	return DefineJSONValue[UnionFailingValue](s)
}
func (v *UnionFailingValue) UnmarshalJSON(raw []byte) error {
	stored, err := DecodeUnion(unionFailingJSON(), raw)
	if err != nil {
		return err
	}
	if _, err := UnionVariant[UnionFailingValue, unionFailingPayload](stored, stored.Tag()); err != nil {
		return err
	}
	v.stored = stored
	return nil
}
func TestUnionNestedCodecFailureRemainsInternal(t *testing.T) {
	limits := JSONLimits{Bytes: 8192, Depth: 8, Nodes: 100, Steps: 500, Issues: 8}
	for _, token := range []string{"panic", "exit"} {
		raw := []byte(`{"kind":"card","token":"` + token + `"}`)
		result, err := unionFailingJSON().Decode(t.Context(), raw, limits)
		if !result.stored.IsZero() || !errors.Is(err, fault.Internal) || !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private") {
			t.Fatal("union codec classification", err)
		}
	}
}
func TestUnionDecodedPayloadStaysByteBounded(t *testing.T) {
	// Native JSON escaping can expand '<' even when the incoming wire is small.
	raw := []byte(`{"kind":"card","token":"` + strings.Repeat("<", jsonwire.MaxBytes/5) + `"}`)
	if len(raw) >= jsonwire.MaxBytes {
		t.Fatal("fixture exceeded input bound")
	}
	if _, err := DecodeUnion(unionFixtureJSON(), raw); err == nil {
		t.Fatal("expanded native payload exceeded its byte bound")
	}
}
