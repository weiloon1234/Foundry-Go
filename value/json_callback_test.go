package value_test

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

type privateJSONError struct{ formatted atomic.Int32 }

func (e *privateJSONError) Error() string {
	e.formatted.Add(1)
	runtime.Goexit()
	return "private JSON"
}
func (e *privateJSONError) Format(fmt.State, rune) {
	e.formatted.Add(1)
	panic("private JSON formatter")
}

type failedJSONMarshal struct{ Cause *privateJSONError }

func (v failedJSONMarshal) MarshalJSON() ([]byte, error) { return nil, v.Cause }

func TestJSONWrapperCompositionRetainsSafeCauses(t *testing.T) {
	cause := &privateJSONError{}
	input := failedJSONMarshal{cause}
	candidates := []any{
		value.Set(input), value.Of(input), value.List[failedJSONMarshal]{input},
		value.Set(value.Of(value.List[failedJSONMarshal]{input})),
	}
	for _, candidate := range candidates {
		_, direct := candidate.(json.Marshaler).MarshalJSON()
		_, transport := value.EncodeJSON(t.Context(), candidate, encodingLimits())
		_, snapshot := value.NewJSON(candidate)
		for _, err := range []error{direct, transport, snapshot} {
			var typed *privateJSONError
			if !errors.Is(err, cause) || !errors.As(err, &typed) || typed != cause {
				t.Fatal("nested encoding lost identity")
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") {
				t.Fatal("private text leaked")
			}
		}
	}
	if cause.formatted.Load() != 0 {
		t.Fatal("private formatter was invoked")
	}
}

var nestedDecodeCause = &privateJSONError{}

type failedNestedJSON struct{}

func (*failedNestedJSON) UnmarshalJSONFrom(*jsontext.Decoder) error { return nestedDecodeCause }

func TestJSONWrapperDecodeRetainsSafeCauses(t *testing.T) {
	var optional value.Optional[value.Nullable[failedNestedJSON]]
	var list value.List[failedNestedJSON]
	failures := []error{optional.UnmarshalJSON([]byte(`{}`)), list.UnmarshalJSON([]byte(`[{}]`))}
	_, err := value.ParseJSON[failedNestedJSON](`{}`)
	failures = append(failures, err)
	_, err = value.ParseJSON[value.List[failedNestedJSON]](`[{}]`)
	failures = append(failures, err)
	for _, err := range failures {
		var typed *privateJSONError
		if !errors.Is(err, nestedDecodeCause) || !errors.As(err, &typed) || typed != nestedDecodeCause {
			t.Fatal("nested decoding lost identity")
		}
		_ = fmt.Sprintf("%v %+v %#v", err, err, err)
	}
	if nestedDecodeCause.formatted.Load() != 0 {
		t.Fatal("private decoder formatter was invoked")
	}
}

func TestListStreamingRetainsCallerMarshalOptions(t *testing.T) {
	custom := jsonv2.WithMarshalers(jsonv2.MarshalFunc[int](func(int) ([]byte, error) { return []byte(`"custom"`), nil }))
	encoded, err := jsonv2.Marshal(value.List[int]{1, 2}, custom)
	if err != nil || string(encoded) != `["custom","custom"]` {
		t.Fatalf("streaming list lost caller codec: %s %v", encoded, err)
	}
}
