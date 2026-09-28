package http_test

import (
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type defaultBytes []byte
type defaultBytesCodec struct{}

func (defaultBytesCodec) Parse(raw string) (defaultBytes, error)    { return defaultBytes(raw), nil }
func (defaultBytesCodec) Format(value defaultBytes) (string, error) { return string(value), nil }

type defaultBytesInput struct{ Value defaultBytes }

func TestQueryDefaultsSnapshotMutableValues(t *testing.T) {
	t.Parallel()
	initial := defaultBytes("initial")
	descriptor := foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam("v", defaultBytesCodec{}, initial, func(q *defaultBytesInput) *defaultBytes { return &q.Value }))
	initial[0] = 'X'
	first, err := descriptor.Decode(t.Context(), "", queryLimits)
	if err != nil || string(first.Value) != "initial" {
		t.Fatalf("default aliases caller: %q %v", first.Value, err)
	}
	first.Value[0] = 'Y'
	second, err := descriptor.Decode(t.Context(), "", queryLimits)
	if err != nil || string(second.Value) != "initial" {
		t.Fatal("default shared mutable state across requests")
	}
	metadata, err := descriptor.Parameters()
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := metadata[0].DefaultURL.Get(); !ok || text != "initial" || metadata[0].Scalar != nil {
		t.Fatal("default fabricated a custom scalar schema")
	}
}

type defaultStringCodec struct {
	parse  func(string) (string, error)
	format func(string) (string, error)
}

func (c defaultStringCodec) Parse(raw string) (string, error) {
	if c.parse != nil {
		return c.parse(raw)
	}
	return raw, nil
}
func (c defaultStringCodec) Format(value string) (string, error) {
	if c.format != nil {
		return c.format(value)
	}
	return value, nil
}

type defaultStringInput struct{ Value string }

func stringDefault(codec foundryhttp.QueryCodec[string], value string) foundryhttp.Query[defaultStringInput] {
	return foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam("v", codec, value, func(q *defaultStringInput) *string { return &q.Value }))
}

func TestQueryDefaultDeclarationOwnsCodecFailures(t *testing.T) {
	t.Parallel()
	for _, codec := range []foundryhttp.QueryCodec[string]{
		nil,
		defaultStringCodec{parse: func(string) (string, error) { return "", errors.New("private-codec") }},
		defaultStringCodec{format: func(string) (string, error) { return "", errors.New("private-codec") }},
		defaultStringCodec{parse: func(string) (string, error) { return "different", nil }},
		defaultStringCodec{format: func(string) (string, error) { panic("private-codec") }},
		defaultStringCodec{parse: func(string) (string, error) { runtime.Goexit(); return "", nil }},
	} {
		descriptor := stringDefault(codec, "initial")
		if descriptor.Validate() == nil {
			t.Fatal("invalid default declaration accepted")
		}
	}
	if stringDefault(defaultStringCodec{}, string([]byte{255})).Validate() == nil {
		t.Fatal("invalid UTF-8 default cannot be represented faithfully in metadata")
	}
	if stringDefault(defaultStringCodec{}, strings.Repeat("x", (16<<10)+1)).Validate() == nil {
		t.Fatal("unbounded default accepted")
	}
	if foundryhttp.DefineQuery(foundryhttp.DefaultQueryParam[defaultStringInput]("v", foundryhttp.StringQuery[string](), "", nil)).Validate() == nil {
		t.Fatal("missing default field selector accepted")
	}
}

func TestDefaultQueryFailureIsNotAttributedToClientInput(t *testing.T) {
	t.Parallel()
	broken := false
	calls := 0
	descriptor := stringDefault(defaultStringCodec{parse: func(raw string) (string, error) {
		calls++
		if broken {
			return "", errors.New("private-codec")
		}
		return raw, nil
	}}, "initial")
	if descriptor.Validate() != nil {
		t.Fatal("valid declaration rejected")
	}
	priorCalls := calls
	// Shared structural validation still precedes every default/field callback.
	for _, input := range []string{"v=a&v=b", "unknown=bad"} {
		if _, err := descriptor.Decode(t.Context(), input, queryLimits); err == nil {
			t.Fatal("ambiguous input accepted")
		}
	}
	if calls != priorCalls {
		t.Fatal("default codec ran before transport validation")
	}
	broken = true
	empty, err := descriptor.Decode(t.Context(), "", queryLimits)
	if !errors.Is(err, fault.Internal) || !reflect.DeepEqual(empty, defaultStringInput{}) {
		t.Fatal("broken server default blamed on client")
	}
	_, err = descriptor.Decode(t.Context(), "v=provided", queryLimits)
	var invalid *foundryhttp.QueryError
	if !errors.As(err, &invalid) || errors.Is(err, fault.Internal) || strings.Contains(err.Error(), "private-codec") {
		t.Fatal("client codec rejection lost safe classification")
	}
}
