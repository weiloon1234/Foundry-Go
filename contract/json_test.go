package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ProfileModel struct{}

type PatchDTO struct {
	ID    model.ID[ProfileModel]                 `json:"id"`
	Email value.Optional[value.Nullable[string]] `json:"email,omitzero"`
	Title value.Optional[string]                 `json:"title,omitzero"`
	Age   uint8                                  `json:"age"`
	Data  any                                    `json:"data,omitempty"`
}

func dtoRoot[T any]() TypeID {
	typ := reflect.TypeFor[T]()
	return TypeID(typ.PkgPath() + "." + typ.Name())
}

func patchDescriptor() JSON[PatchDTO] {
	root := dtoRoot[PatchDTO]()
	return DefineJSON[PatchDTO](Schema{Root: root, Types: []Type{
		{ID: root, Kind: ObjectKind, Properties: []Property{
			{Name: "id", Type: "id", Required: true},
			{Name: "email", Type: "nullable_text"},
			{Name: "title", Type: "text"},
			{Name: "age", Type: "age", Required: true},
			{Name: "data", Type: "data"},
		}},
		{ID: "id", Kind: StringKind, Format: UUIDFormat},
		{ID: "text", Kind: StringKind},
		{ID: "nullable_text", Kind: AliasKind, Element: "text", Nullable: true},
		{ID: "age", Kind: IntegerKind, Bits: 8},
		{ID: "data", Kind: DynamicKind, Nullable: true},
	}})
}

func dtoLimits() JSONLimits {
	return JSONLimits{Bytes: 4096, Depth: 16, Nodes: 500, Steps: 2000, Issues: 20}
}

func TestJSONDecodePreservesConcreteValuesAndPatchStates(t *testing.T) {
	descriptor := patchDescriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, extra string
		set, null   bool
		value       string
	}{
		{"omitted", "", false, false, ""},
		{"null", `,"email":null`, true, true, ""},
		{"empty", `,"email":""`, true, false, ""},
		{"present", `,"email":"A@example.test"`, true, false, "A@example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := []byte(`{"id":"0193fd8c-2075-7000-8000-000000000001","age":0,"data":{"exact":9007199254740993}` + test.extra + `}`)
			result, err := descriptor.Decode(context.Background(), data, dtoLimits())
			if err != nil {
				t.Fatal(err)
			}
			expectedID, err := model.ParseID[ProfileModel]("0193fd8c-2075-7000-8000-000000000001")
			if err != nil || result.ID != expectedID || result.Age != 0 {
				t.Fatal("concrete identity or zero value changed")
			}
			if result.Email.IsSet() != test.set {
				t.Fatal("omitted state changed")
			}
			if test.set {
				nullable, _ := result.Email.Get()
				if nullable.IsNull() != test.null {
					t.Fatal("null state changed")
				}
				if !test.null {
					got, ok := nullable.Get()
					if !ok || got != test.value {
						t.Fatal("present value changed")
					}
				}
			}
			if result.Title.IsSet() {
				t.Fatal("missing optional title became present")
			}
			if got := result.Data.(map[string]any)["exact"]; got != json.Number("9007199254740993") {
				t.Fatalf("dynamic number changed: %v", got)
			}
		})
	}
}

func TestJSONDecodeRejectsInputWithoutPartialDTO(t *testing.T) {
	descriptor := patchDescriptor()
	base := `"id":"0193fd8c-2075-7000-8000-000000000001","age":1`
	for _, data := range []string{
		`{` + base + `,"title":null}`,
		`{` + base + `,"age":2}`,
		`{` + base + `,"secret name":"private value"}`,
		`{` + base + `,"email":12}`,
		`{"id":"secret invalid ID","age":1}`,
		`{"id":"0193fd8c-2075-7000-8000-000000000001","age":256}`,
		`{"ID":"0193fd8c-2075-7000-8000-000000000001","age":1}`,
		`{` + base + `,"title":"\ud800"}`,
		`{` + base + `} {}`,
		`null`,
	} {
		result, err := descriptor.Decode(context.Background(), []byte(data), dtoLimits())
		var failure *DecodeError
		if !reflect.DeepEqual(result, PatchDTO{}) || !errors.As(err, &failure) || !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid input returned partial result or unexpected error: %v", err)
		}
		if strings.Contains(fmt.Sprintf("%v %#v", err, err), "secret") || strings.Contains(err.Error(), "private") {
			t.Fatal("received input leaked through error formatting")
		}
		if issues := failure.Issues(); len(issues) > 0 {
			issues[0].Path = "mutated"
			if failure.Issues()[0].Path == "mutated" {
				t.Fatal("caller changed owned issues")
			}
		}
	}
}

type CodecText string

type hostileCodecError struct{}

var codecInputError = errors.New("private codec detail")

func (hostileCodecError) Error() string { return "private codec detail" }
func (hostileCodecError) Is(error) bool { panic("codec error Is must not run") }

var cancelCodecTest context.CancelFunc
var blockCodecTest struct{ entered, release chan struct{} }

func (text *CodecText) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*text = "partially assigned"
	switch raw {
	case "panic":
		panic("private panic payload")
	case "goexit":
		runtime.Goexit()
	case "error":
		return codecInputError
	case "hostile":
		return hostileCodecError{}
	case "cancel":
		cancelCodecTest()
	case "cancelpanic":
		cancelCodecTest()
		panic("private panic after cancellation")
	case "cancelgoexit":
		cancelCodecTest()
		runtime.Goexit()
	case "block":
		close(blockCodecTest.entered)
		<-blockCodecTest.release
	}
	*text = CodecText(strings.ToUpper(raw))
	return nil
}

type CodecDTO struct {
	Text CodecText `json:"text"`
}

func codecDescriptor() JSON[CodecDTO] {
	root := dtoRoot[CodecDTO]()
	return DefineJSON[CodecDTO](Schema{Root: root, Types: []Type{
		{ID: root, Kind: ObjectKind, Properties: []Property{{Name: "text", Type: "text", Required: true}}},
		{ID: "text", Kind: StringKind},
	}})
}

func TestJSONCodecFailuresAreContained(t *testing.T) {
	descriptor := codecDescriptor()
	for _, mode := range []string{"panic", "goexit", "error", "hostile"} {
		t.Run(mode, func(t *testing.T) {
			result, err := descriptor.Decode(context.Background(), []byte(`{"text":"`+mode+`"}`), dtoLimits())
			if result != (CodecDTO{}) || err == nil {
				t.Fatal("codec failure returned partial result")
			}
			if strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") {
				t.Fatal("codec detail leaked")
			}
			if mode == "error" {
				var failure *DecodeError
				if !errors.As(err, &failure) || !errors.Is(err, codecInputError) {
					t.Fatalf("codec cause was lost: %T %v", err, err)
				}
			} else if !errors.Is(err, fault.Internal) || !errors.Is(err, fault.Panicked) {
				t.Fatalf("codec panic/Goexit not internal: %v", err)
			}
		})
	}
	result, err := descriptor.Decode(context.Background(), []byte(`{"text":"valid"}`), dtoLimits())
	if err != nil || result.Text != "VALID" {
		t.Fatalf("valid codec did not run: %v", err)
	}
}

func TestJSONCancellationRetainsCodecOwnership(t *testing.T) {
	descriptor := codecDescriptor()
	ctx, cancel := context.WithCancel(context.Background())
	cancelCodecTest = cancel
	defer func() { cancelCodecTest = nil }()
	result, err := descriptor.Decode(ctx, []byte(`{"text":"cancel"}`), dtoLimits())
	if result != (CodecDTO{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("codec cancellation: %v", err)
	}
	result, err = descriptor.Decode(ctx, []byte(`{"text":"panic"}`), dtoLimits())
	if result != (CodecDTO{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancellation ran codec: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	blockCodecTest.entered = make(chan struct{})
	blockCodecTest.release = make(chan struct{})
	var released sync.Once
	release := func() { released.Do(func() { close(blockCodecTest.release) }) }
	defer release()
	done := make(chan error, 1)
	go func() {
		result, err := descriptor.Decode(ctx, []byte(`{"text":"block"}`), dtoLimits())
		if result != (CodecDTO{}) {
			err = fmt.Errorf("partial result returned")
		}
		done <- err
	}()
	<-blockCodecTest.entered
	cancel()
	select {
	case <-done:
		t.Fatal("decode abandoned an active codec")
	default:
	}
	release()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("owned codec did not finish canceled: %v", err)
	}
}

func TestJSONCancellationDoesNotHideCodecFailure(t *testing.T) {
	defer func() { cancelCodecTest = nil }()
	for _, mode := range []string{"cancelpanic", "cancelgoexit"} {
		ctx, cancel := context.WithCancel(context.Background())
		cancelCodecTest = cancel
		result, err := codecDescriptor().Decode(ctx, []byte(`{"text":"`+mode+`"}`), dtoLimits())
		cancel()
		if result != (CodecDTO{}) || !errors.Is(err, fault.Panicked) || !errors.Is(err, fault.Internal) {
			t.Fatalf("cancellation hid codec failure: %v", err)
		}
	}
}

type PersistenceDTO struct{ Password string }

func (PersistenceDTO) FoundryIdentity() (model.Identity, error) {
	panic("identity method must not be invoked")
}

type PointerPersistenceDTO struct{ Password string }

func (*PointerPersistenceDTO) FoundryIdentity() (model.Identity, error) {
	panic("identity method must not be invoked")
}

func TestJSONDeclarationsRejectWrongRootsAndModels(t *testing.T) {
	for _, err := range []error{
		(JSON[PatchDTO]{}).Validate(),
		DefineJSON[PatchDTO](Schema{Root: "different", Types: []Type{{ID: "different", Kind: ObjectKind}}}).Validate(),
		DefineJSON[*PatchDTO](Schema{Root: dtoRoot[*PatchDTO](), Types: []Type{{ID: dtoRoot[*PatchDTO](), Kind: ObjectKind}}}).Validate(),
		DefineJSON[PersistenceDTO](Schema{Root: dtoRoot[PersistenceDTO](), Types: []Type{{ID: dtoRoot[PersistenceDTO](), Kind: ObjectKind}}}).Validate(),
		DefineJSON[PointerPersistenceDTO](Schema{Root: dtoRoot[PointerPersistenceDTO](), Types: []Type{{ID: dtoRoot[PointerPersistenceDTO](), Kind: ObjectKind}}}).Validate(),
		DefineJSON[PatchDTO](Schema{Root: dtoRoot[PatchDTO](), Types: []Type{{ID: dtoRoot[PatchDTO](), Kind: StringKind}}}).Validate(),
	} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid DTO declaration accepted: %v", err)
		}
	}
	for _, limits := range []JSONLimits{{}, {Bytes: 1, Depth: -1, Nodes: 1, Steps: 1, Issues: 1}, {Bytes: 1, Depth: MaxJSONDepth + 1, Nodes: 1, Steps: 1, Issues: 1}} {
		if !errors.Is(limits.Validate(), fault.Invalid) {
			t.Fatal("invalid limits accepted")
		}
	}
}

func TestJSONDescriptorCanBeShared(t *testing.T) {
	descriptor := patchDescriptor()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				result, err := descriptor.Decode(context.Background(), []byte(`{"id":"0193fd8c-2075-7000-8000-000000000001","age":10}`), dtoLimits())
				if err != nil || result.Age != 10 {
					t.Errorf("concurrent decode failed: %v", err)
					return
				}
				description, err := descriptor.Description()
				if err != nil {
					t.Error(err)
					return
				}
				description.Types[0].ID = "changed"
			}
		})
	}
	wg.Wait()
}

func TestJSONTransportBoundsAreEnforced(t *testing.T) {
	descriptor := patchDescriptor()
	input := []byte(`{"id":"0193fd8c-2075-7000-8000-000000000001","age":1}`)
	for _, alter := range []func(*JSONLimits){
		func(l *JSONLimits) { l.Bytes = len(input) - 1 },
		func(l *JSONLimits) { l.Depth = 0 },
		func(l *JSONLimits) { l.Nodes = 3 },
		func(l *JSONLimits) { l.Steps = 1 },
	} {
		limits := dtoLimits()
		alter(&limits)
		result, err := descriptor.Decode(context.Background(), input, limits)
		var failure *DecodeError
		if !reflect.DeepEqual(result, PatchDTO{}) || !errors.As(err, &failure) {
			t.Fatalf("bound did not reject input: %v", err)
		}
	}
	limits := dtoLimits()
	limits.Issues = 1
	_, err := descriptor.Decode(context.Background(), []byte(`{}`), limits)
	var failure *DecodeError
	if !errors.As(err, &failure) || len(failure.Issues()) != 1 {
		t.Fatalf("issue bound failed: %v", err)
	}
	if _, err := descriptor.Decode(nil, input, dtoLimits()); !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil context accepted: %v", err)
	}
}

func TestDTOIdentityPreservesMainSourceNamespaces(t *testing.T) {
	for _, test := range []struct {
		runtimePackage, name string
		id                   TypeID
		want                 bool
	}{
		{"example.test/api", "Input", "example.test/api.Input", true},
		{"example.test/api", "Input", "example.test/other.Input", false},
		{"example.test/api", "Input", "example.test/api.Other", false},
		{"main", "Input", "example.test/cmd/api.Input", true},
		{"main", "Input", "example.test/cmd/api.Other", false},
		{"main", "Input", "Input", false},
		{"main", "Input", ".Input", false},
		{"main", "Input", "invalid namespace.Input", false},
		{"main", "Box[domain.User]", "example.test/cmd/api.Box[domain.User]", true},
	} {
		if got := dtoIdentityMatches(test.runtimePackage, test.name, test.id); got != test.want {
			t.Fatalf("identity match for %q/%q/%q = %v", test.runtimePackage, test.name, test.id, got)
		}
	}
}
