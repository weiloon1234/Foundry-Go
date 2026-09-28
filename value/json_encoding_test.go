package value_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func encodingLimits() value.JSONEncodingLimits {
	return value.JSONEncodingLimits{Bytes: 4096, Depth: 16, Nodes: 500, Steps: 2000}
}

type EncodingPayload struct {
	Name value.Optional[string]         `json:"name,omitzero"`
	Data value.Nullable[map[string]any] `json:"data"`
}

func TestEncodeJSONPreservesOrdinaryTransportValues(t *testing.T) {
	input := EncodingPayload{Name: value.Set("a\x00b"), Data: value.Of(map[string]any{"exact": json.Number("9007199254740993")})}
	data, err := value.EncodeJSON(context.Background(), input, encodingLimits())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(input)
	if err != nil || string(data) != string(expected) {
		t.Fatalf("ordinary JSON representation changed: %s %v", data, err)
	}
	var decoded EncodingPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, decoded) {
		t.Fatal("encoding lost exact values")
	}
	data[0] = '['
	fresh, err := value.EncodeJSON(context.Background(), input, encodingLimits())
	if err != nil || fresh[0] != '{' {
		t.Fatal("caller changed a retained encoding buffer")
	}
}

func TestEncodeJSONHonorsEmptyDTOAndPromotedFields(t *testing.T) {
	type Empty struct {
		Name value.Optional[string] `json:"name,omitzero"`
		Note string                 `json:"note,omitempty"`
	}
	limits := encodingLimits()
	limits.Bytes = 2
	limits.Depth = 0
	limits.Nodes = 1
	data, err := value.EncodeJSON(context.Background(), Empty{}, limits)
	if err != nil || string(data) != "{}" {
		t.Fatalf("omitted fields consumed wire bounds: %s %v", data, err)
	}
	type Embedded struct {
		Label string `json:"label"`
	}
	type Response struct{ *Embedded }
	data, err = value.EncodeJSON(context.Background(), Response{&Embedded{"ok"}}, encodingLimits())
	if err != nil || string(data) != `{"label":"ok"}` {
		t.Fatalf("promoted fields changed: %s %v", data, err)
	}
}

func TestEncodeJSONRejectsBoundsUnicodeAndNativeCycles(t *testing.T) {
	type Node struct {
		Next *Node `json:"next"`
	}
	cycle := &Node{}
	cycle.Next = cycle
	inputs := []any{strings.Repeat("x", 4097), []byte(strings.Repeat("x", 4097)), string([]byte{0xff}), cycle}
	for _, input := range inputs {
		data, err := value.EncodeJSON(context.Background(), input, encodingLimits())
		if err == nil || data != nil {
			t.Fatal("invalid encoding returned output")
		}
	}
	for _, alter := range []func(*value.JSONEncodingLimits){
		func(l *value.JSONEncodingLimits) { l.Bytes = 5 },
		func(l *value.JSONEncodingLimits) { l.Depth = 0 },
		func(l *value.JSONEncodingLimits) { l.Nodes = 2 },
		func(l *value.JSONEncodingLimits) { l.Steps = 1 },
	} {
		limits := encodingLimits()
		alter(&limits)
		data, err := value.EncodeJSON(context.Background(), map[string]any{"name": "hello"}, limits)
		if err == nil || data != nil {
			t.Fatal("encoding bypassed a resource bound")
		}
	}
	if _, err := value.EncodeJSON[any](nil, nil, encodingLimits()); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted")
	}
	if _, err := value.EncodeJSON(context.Background(), true, value.JSONEncodingLimits{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid limits accepted")
	}
}

type EncodingCodec struct{ Mode string }

var encodingCodecCancel context.CancelFunc
var encodingCodecBlock struct{ entered, release chan struct{} }
var encodingCodecCause = errors.New("private codec cause")

func (v EncodingCodec) MarshalJSON() ([]byte, error) {
	switch v.Mode {
	case "panic":
		panic("private codec panic")
	case "goexit":
		runtime.Goexit()
	case "error":
		return nil, encodingCodecCause
	case "cancelpanic":
		encodingCodecCancel()
		panic("private canceled codec panic")
	case "cancelgoexit":
		encodingCodecCancel()
		runtime.Goexit()
	case "block":
		close(encodingCodecBlock.entered)
		<-encodingCodecBlock.release
	case "utf8":
		return []byte{'"', 0xff, '"'}, nil
	case "duplicate":
		return []byte(`{"same":1,"same":2}`), nil
	}
	return []byte(`"ok"`), nil
}

func TestEncodeJSONContainsCodecFailures(t *testing.T) {
	defer func() { encodingCodecCancel = nil }()
	for _, mode := range []string{"panic", "goexit", "error", "cancelpanic", "cancelgoexit", "utf8", "duplicate"} {
		ctx, cancel := context.WithCancel(context.Background())
		encodingCodecCancel = cancel
		data, err := value.EncodeJSON(ctx, EncodingCodec{mode}, encodingLimits())
		cancel()
		if err == nil || data != nil || strings.Contains(err.Error(), "private") {
			t.Fatal("codec failure exposed output or details")
		}
		if strings.Contains(mode, "panic") || strings.Contains(mode, "goexit") {
			if !errors.Is(err, fault.Panicked) || !errors.Is(err, fault.Internal) {
				t.Fatalf("codec failure was lost: %v", err)
			}
		}
		if mode == "error" && !errors.Is(err, encodingCodecCause) {
			t.Fatal("codec cause was lost")
		}
	}
}

func TestEncodeJSONCancellationRetainsCodecOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	encodingCodecBlock.entered = make(chan struct{})
	encodingCodecBlock.release = make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(encodingCodecBlock.release) }) }
	defer release()
	done := make(chan error, 1)
	go func() {
		data, err := value.EncodeJSON(ctx, EncodingCodec{"block"}, encodingLimits())
		if data != nil {
			err = errors.New("partial output")
		}
		done <- err
	}()
	<-encodingCodecBlock.entered
	cancel()
	select {
	case <-done:
		t.Fatal("encoding abandoned its codec")
	default:
	}
	release()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("codec did not finish canceled: %v", err)
	}
}

func BenchmarkEncodeJSONRejectOversizedNativeString(b *testing.B) {
	for _, size := range []int{1 << 20, 16 << 20} {
		input := strings.Repeat("x", size)
		b.Run(strconv.Itoa(size/(1<<20))+"MiB", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := value.EncodeJSON(context.Background(), input, encodingLimits()); err == nil {
					b.Fatal("oversized string accepted")
				}
			}
		})
	}
}

type EncodingFalseZero bool

func (EncodingFalseZero) IsZero() bool { return false }

type EncodingOmitted struct{ Text string }

func (*EncodingOmitted) IsZero() bool { return true }

func TestEncodeJSONOmissionMatchesNativeOptionOrderAndPointerMethods(t *testing.T) {
	type Flags struct {
		First  EncodingFalseZero `json:"first,omitzero,omitempty"`
		Second EncodingFalseZero `json:"second,omitempty,omitzero"`
		Hidden EncodingOmitted   `json:"hidden,omitzero"`
	}
	input := Flags{Hidden: EncodingOmitted{Text: strings.Repeat("x", 8192)}}
	expected, err := json.Marshal(input)
	if err != nil || string(expected) != "{}" {
		t.Fatalf("native omission assumption is incorrect: %s %v", expected, err)
	}
	limits := encodingLimits()
	limits.Bytes = 2
	limits.Depth = 0
	limits.Nodes = 1
	data, err := value.EncodeJSON(context.Background(), input, limits)
	if err != nil || string(data) != string(expected) {
		t.Fatalf("bounded omission differs from native encoding: %s %v", data, err)
	}
}

func TestEncodeJSONPreservesNilWrapperPointers(t *testing.T) {
	var optional *value.Optional[string]
	var nullable *value.Nullable[string]
	for _, input := range []any{optional, nullable, (*value.JSON[string])(nil)} {
		data, err := value.EncodeJSON(context.Background(), input, encodingLimits())
		if err != nil || string(data) != "null" {
			t.Fatalf("nil wrapper pointer changed: %s %v", data, err)
		}
	}
}
