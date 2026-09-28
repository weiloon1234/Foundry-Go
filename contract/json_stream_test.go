package contract

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type StreamingValue struct {
	number  json.Number
	dynamic any
}

func (v StreamingValue) MarshalJSONTo(enc *jsontext.Encoder) error {
	return jsonv2.MarshalEncode(enc, struct {
		Number  json.Number
		Dynamic any
	}{v.number, v.dynamic})
}
func (v *StreamingValue) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var data struct {
		Number  json.Number
		Dynamic any
	}
	if err := jsonv2.UnmarshalDecode(dec, &data); err != nil {
		return err
	}
	v.number, v.dynamic = data.Number, data.Dynamic
	return nil
}

func streamingValueContract() JSON[StreamingValue] {
	id := TypeID("github.com/weiloon1234/Foundry-Go/contract.StreamingValue")
	return DefineJSONValue[StreamingValue](Schema{Root: id, Types: []Type{
		{ID: id, Kind: ObjectKind, Properties: []Property{{Name: "Number", Type: "number", Required: true}, {Name: "Dynamic", Type: "dynamic", Required: true}}},
		{ID: "number", Kind: NumberKind}, {ID: "dynamic", Kind: DynamicKind, Nullable: true},
	}})
}

func TestStreamingJSONValuePreservesExactNumbersAndOptions(t *testing.T) {
	d := streamingValueContract()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	input := `{"Number":1e10000,"Dynamic":{"count":9007199254740993}}`
	got, err := d.Decode(t.Context(), []byte(input), dtoLimits())
	if err != nil || got.number != json.Number("1e10000") {
		t.Fatal("streaming exact number", err)
	}
	values, ok := got.dynamic.(map[string]any)
	if !ok || values["count"] != json.Number("9007199254740993") {
		t.Fatal("nested streaming decoder lost UseNumber", got.dynamic)
	}
	output, err := d.Encode(t.Context(), got, dtoLimits())
	if err != nil || !strings.Contains(string(output), "1e10000") || !strings.Contains(string(output), "9007199254740993") {
		t.Fatal("streaming output precision", string(output), err)
	}
	if _, err := d.Decode(t.Context(), []byte(input+" {}"), dtoLimits()); err == nil {
		t.Fatal("streaming trailing value accepted")
	}
	if result, err := d.Decode(t.Context(), []byte(`{"Number":1,"Dynamic":null,"Unknown":"private"}`), dtoLimits()); err == nil || result.number != "" {
		t.Fatal("unknown field/partial streaming value", err)
	}
}

type StreamingPrecedence string

func (v StreamingPrecedence) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(string(v)))
}
func (v *StreamingPrecedence) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var text string
	if err := jsonv2.UnmarshalDecode(dec, &text); err != nil {
		return err
	}
	*v = StreamingPrecedence(text)
	return nil
}
func (StreamingPrecedence) MarshalJSON() ([]byte, error) { panic("legacy encoder should not run") }
func (*StreamingPrecedence) UnmarshalJSON([]byte) error  { panic("legacy decoder should not run") }

type StreamingFallback string

func (StreamingFallback) MarshalJSONTo(*jsontext.Encoder) error      { return errors.ErrUnsupported }
func (*StreamingFallback) UnmarshalJSONFrom(*jsontext.Decoder) error { return errors.ErrUnsupported }
func (v StreamingFallback) MarshalJSON() ([]byte, error)             { return json.Marshal(string(v)) }
func (v *StreamingFallback) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*v = StreamingFallback(text)
	return nil
}

func TestStreamingJSONNativePrecedenceAndFallback(t *testing.T) {
	precedence := ScalarJSON(DefineScalar[StreamingPrecedence](Type{ID: "github.com/weiloon1234/Foundry-Go/contract.StreamingPrecedence", Kind: StringKind}))
	got, err := precedence.Decode(t.Context(), []byte(`"native"`), dtoLimits())
	if err != nil || got != "native" {
		t.Fatal("streaming precedence", err)
	}
	output, err := precedence.Encode(t.Context(), got, dtoLimits())
	if err != nil || string(output) != `"native"` {
		t.Fatal("streaming encode precedence", err)
	}
	fallback := ScalarJSON(DefineScalar[StreamingFallback](Type{ID: "github.com/weiloon1234/Foundry-Go/contract.StreamingFallback", Kind: StringKind}))
	legacy, err := fallback.Decode(t.Context(), []byte(`"fallback"`), dtoLimits())
	if err != nil || legacy != "fallback" {
		t.Fatal("native unsupported fallback", err)
	}
	output, err = fallback.Encode(t.Context(), legacy, dtoLimits())
	if err != nil || string(output) != `"fallback"` {
		t.Fatal("native encoder fallback", err)
	}
}

type StreamingNoConsumption string

func (v StreamingNoConsumption) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(string(v)))
}
func (*StreamingNoConsumption) UnmarshalJSONFrom(*jsontext.Decoder) error { return nil }

type StreamingWrongPointer string

func (*StreamingWrongPointer) MarshalJSONTo(*jsontext.Encoder) error     { return nil }
func (*StreamingWrongPointer) UnmarshalJSONFrom(*jsontext.Decoder) error { return nil }

func TestStreamingJSONRequiresSingularConsumptionAndValueEncoder(t *testing.T) {
	d := ScalarJSON(DefineScalar[StreamingNoConsumption](Type{ID: "github.com/weiloon1234/Foundry-Go/contract.StreamingNoConsumption", Kind: StringKind}))
	result, err := d.Decode(t.Context(), []byte(`"input"`), dtoLimits())
	var failure *DecodeError
	if !errors.As(err, &failure) || result != "" {
		t.Fatal("non-consuming native decoder accepted", err)
	}
	invalid := ScalarJSON(DefineScalar[StreamingWrongPointer](Type{ID: "github.com/weiloon1234/Foundry-Go/contract.StreamingWrongPointer", Kind: StringKind}))
	if invalid.Validate() == nil {
		t.Fatal("pointer-only streaming encoder accepted")
	}
}

var streamStarted, streamRelease chan struct{}

type StreamingFailure string

func (v StreamingFailure) MarshalJSONTo(enc *jsontext.Encoder) error {
	switch v {
	case "panic":
		panic("private stream")
	case "goexit":
		runtime.Goexit()
	}
	return enc.WriteToken(jsontext.String(string(v)))
}
func (v *StreamingFailure) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var text string
	if err := jsonv2.UnmarshalDecode(dec, &text); err != nil {
		return err
	}
	switch text {
	case "panic":
		panic("private stream")
	case "goexit":
		runtime.Goexit()
	case "wait", "wait-panic":
		close(streamStarted)
		<-streamRelease
		if text == "wait-panic" {
			panic("private stream")
		}
	}
	*v = StreamingFailure(text)
	return nil
}

func TestStreamingJSONCallbackOwnershipAndFailures(t *testing.T) {
	d := ScalarJSON(DefineScalar[StreamingFailure](Type{ID: "github.com/weiloon1234/Foundry-Go/contract.StreamingFailure", Kind: StringKind}))
	for _, text := range []string{"panic", "goexit"} {
		input, err := d.Decode(t.Context(), []byte("\""+text+"\""), dtoLimits())
		if input != "" || !errors.Is(err, fault.Internal) || strings.Contains(err.Error(), "private") {
			t.Fatal("streaming decode callback", err)
		}
		output, err := d.Encode(t.Context(), StreamingFailure(text), dtoLimits())
		if output != nil || !errors.Is(err, fault.Internal) || strings.Contains(err.Error(), "private") {
			t.Fatal("streaming encode callback", err)
		}
	}
	for _, text := range []string{"wait", "wait-panic"} {
		streamStarted, streamRelease = make(chan struct{}), make(chan struct{})
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() { _, err := d.Decode(ctx, []byte("\""+text+"\""), dtoLimits()); result <- err }()
		select {
		case <-streamStarted:
		case err := <-result:
			cancel()
			t.Fatal("streaming decoder did not start", err)
		}
		cancel()
		select {
		case err := <-result:
			t.Fatal("running native codec abandoned", err)
		case <-time.After(20 * time.Millisecond):
		}
		close(streamRelease)
		err := <-result
		if text == "wait" && !errors.Is(err, context.Canceled) {
			t.Fatal("streaming cancellation", err)
		}
		if text == "wait-panic" && !errors.Is(err, fault.Internal) {
			t.Fatal("cancellation hid streaming panic", err)
		}
	}
}
