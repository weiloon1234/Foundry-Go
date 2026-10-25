package value_test

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

type privateKeyError struct{ formatted atomic.Int32 }

func (e *privateKeyError) Error() string          { e.formatted.Add(1); panic("private key error") }
func (e *privateKeyError) Format(fmt.State, rune) { e.formatted.Add(1); panic("private key formatter") }

type failedTextKey struct{ cause *privateKeyError }

func (k failedTextKey) MarshalText() ([]byte, error) { return nil, k.cause }
func (*failedTextKey) UnmarshalText([]byte) error    { return nil }

func TestJSONMapKeyFailuresAreSafeBeforeNativeEncoding(t *testing.T) {
	cause := &privateKeyError{}
	input := map[failedTextKey]bool{{cause}: true}
	_, direct := value.NewJSON(input)
	_, nested := value.NewJSON(struct{ Items []map[failedTextKey]bool }{[]map[failedTextKey]bool{input}})
	_, transport := value.EncodeJSON(t.Context(), input, encodingLimits())
	for _, err := range []error{direct, nested, transport} {
		var retained *privateKeyError
		if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
			t.Fatal("map-key encoding lost its cause")
		}
		_ = fmt.Sprintf("%v %+v %#v", err, err, err)
	}
	if cause.formatted.Load() != 0 {
		t.Fatal("map-key error formatted inside native encoding")
	}
}

type stringTextKey string

func (stringTextKey) MarshalText() ([]byte, error)       { return []byte("mapped"), nil }
func (k *stringTextKey) UnmarshalText(text []byte) error { *k = stringTextKey(text); return nil }
func TestJSONKeyEncodingRetainsStringKeyTextMethod(t *testing.T) {
	doc, err := value.NewJSON(map[stringTextKey]int{"original": 1})
	if err != nil {
		t.Fatal(err)
	}
	text, err := doc.Text()
	if err != nil || text != "{\"mapped\":1}" {
		t.Fatal("string key precedence changed")
	}
}

type pointerStringKey string

func (k *pointerStringKey) MarshalText() ([]byte, error) { return []byte("mapped"), nil }

func TestJSONKeyEncodingRetainsPointerKeyMethods(t *testing.T) {
	key := pointerStringKey("original")
	encoded, err := value.EncodeJSON(t.Context(), map[*pointerStringKey]int{&key: 1}, encodingLimits())
	if err != nil || string(encoded) != "{\"mapped\":1}" {
		t.Fatal("pointer key method changed", err)
	}
}

type failedAppendKey struct{ cause *privateKeyError }

func (k failedAppendKey) AppendText([]byte) ([]byte, error) { return nil, k.cause }
func (*failedAppendKey) UnmarshalText([]byte) error         { return nil }

type dualTextKey struct{}

func (dualTextKey) AppendText(dst []byte) ([]byte, error) { return append(dst, "append"...), nil }
func (dualTextKey) MarshalText() ([]byte, error)          { panic("AppendText must take precedence") }
func (*dualTextKey) UnmarshalText([]byte) error           { return nil }

func TestJSONKeyAppenderRedactionAndPrecedence(t *testing.T) {
	cause := &privateKeyError{}
	input := map[failedAppendKey]bool{{cause}: true}
	_, snapshotErr := value.NewJSON(input)
	_, transportErr := value.EncodeJSON(t.Context(), input, encodingLimits())
	for _, err := range []error{snapshotErr, transportErr} {
		var retained *privateKeyError
		if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
			t.Fatal("appender cause lost")
		}
		_ = fmt.Sprintf("%v %+v %#v", err, err, err)
	}
	if cause.formatted.Load() != 0 {
		t.Fatal("appender error formatted")
	}
	encoded, err := value.EncodeJSON(t.Context(), map[dualTextKey]bool{{}: true}, encodingLimits())
	if err != nil || string(encoded) != "{\"append\":true}" {
		t.Fatal("native appender precedence changed", err)
	}
	doc, err := value.NewJSON(map[dualTextKey]bool{{}: true})
	if err != nil {
		t.Fatal(err)
	}
	text, err := doc.Text()
	if err != nil || text != string(encoded) {
		t.Fatal("typed and transport key codecs disagree")
	}
}

var privateJSONDecodeCause = &privateKeyError{}

type failedJSONDecode struct{}

func (*failedJSONDecode) UnmarshalJSON([]byte) error { return privateJSONDecodeCause }
func TestJSONDecodingRetainsPrivateCause(t *testing.T) {
	_, err := value.ParseJSON[failedJSONDecode]("{}")
	if !errors.Is(err, privateJSONDecodeCause) {
		t.Fatal("typed JSON decoder cause lost")
	}
	_ = fmt.Sprintf("%v %+v %#v", err, err, err)
	if privateJSONDecodeCause.formatted.Load() != 0 {
		t.Fatal("typed JSON decoder error formatted")
	}
}
