package contract

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
	"github.com/weiloon1234/Foundry-Go/model"
)

type TransportCode string

func (v TransportCode) MarshalText() ([]byte, error) {
	if !strings.HasPrefix(string(v), "code_") {
		return nil, errors.New("private code detail")
	}
	return []byte(v), nil
}
func (v *TransportCode) UnmarshalText(data []byte) error {
	candidate := TransportCode(data)
	if _, err := candidate.MarshalText(); err != nil {
		return err
	}
	*v = candidate
	return nil
}

func customCodeDescription() JSON[TransportCode] {
	return DefineJSONValue[TransportCode](Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.TransportCode", Types: []Type{
		{ID: "github.com/weiloon1234/Foundry-Go/contract.TransportCode", Kind: StringKind},
	}})
}

type CustomValueDTO struct{ Code TransportCode }

func customValueDTO(factory func() JSON[TransportCode]) JSON[CustomValueDTO] {
	return DefineJSON[CustomValueDTO](Schema{
		Root: "github.com/weiloon1234/Foundry-Go/contract.CustomValueDTO",
		Types: []Type{
			{ID: "github.com/weiloon1234/Foundry-Go/contract.CustomValueDTO", Kind: ObjectKind, Properties: []Property{
				{Name: "Code", Type: "github.com/weiloon1234/Foundry-Go/contract.TransportCode", Required: true},
			}},
			JSONType("github.com/weiloon1234/Foundry-Go/contract.TransportCode", factory),
		},
	})
}

func TestCustomJSONValueUsesNativeCodecAndOwnedMetadata(t *testing.T) {
	calls := 0
	descriptor := customValueDTO(func() JSON[TransportCode] { calls++; return customCodeDescription() })
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	first, err := descriptor.Description()
	if err != nil {
		t.Fatal(err)
	}
	for i := range first.Types {
		first.Types[i].ID = "changed"
	}
	second, err := descriptor.Description()
	if err != nil || second.Types[0].ID == "changed" {
		t.Fatal("shared metadata", err)
	}
	input, err := descriptor.Decode(t.Context(), []byte(`{"Code":"code_123"}`), dtoLimits())
	if err != nil || input.Code != "code_123" {
		t.Fatal("native codec not used", err)
	}
	output, err := descriptor.Encode(t.Context(), input, dtoLimits())
	if err != nil || string(output) != `{"Code":"code_123"}` {
		t.Fatal("custom value changed", string(output), err)
	}
	if calls != 1 {
		t.Fatal("factory executed during request work", calls)
	}
	for _, data := range []string{`{"Code":123}`, `{"Code":"invalid"}`} {
		invalid, err := descriptor.Decode(t.Context(), []byte(data), dtoLimits())
		if err == nil || invalid.Code != "" {
			t.Fatal("invalid or partial custom value", err)
		}
		if strings.Contains(err.Error(), "private") {
			t.Fatal("codec detail leaked")
		}
	}
	if data, err := descriptor.Encode(t.Context(), CustomValueDTO{Code: "invalid"}, dtoLimits()); err == nil || data != nil {
		t.Fatal("invalid output escaped", err)
	}
}

func TestCustomJSONContractFactoryFailureAndConflictingNodes(t *testing.T) {
	for name, factory := range map[string]func() JSON[TransportCode]{
		"nil":    nil,
		"zero":   func() JSON[TransportCode] { return JSON[TransportCode]{} },
		"panic":  func() JSON[TransportCode] { panic("private factory detail") },
		"goexit": func() JSON[TransportCode] { runtime.Goexit(); return JSON[TransportCode]{} },
	} {
		t.Run(name, func(t *testing.T) {
			descriptor := customValueDTO(factory)
			if err := descriptor.Validate(); err == nil || !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid factory accepted", err)
			}
			if value, err := descriptor.Decode(t.Context(), []byte(`{"Code":"code_ok"}`), dtoLimits()); err == nil || value.Code != "" {
				t.Fatal("invalid factory decoded")
			}
		})
	}
	value := DefineJSONValue[TransportCode](Schema{Root: "wrong.TransportCode", Types: []Type{{ID: "wrong.TransportCode", Kind: StringKind}}})
	if value.Validate() == nil {
		t.Fatal("unrelated identity accepted")
	}
	// Shared non-root declarations normalize before comparison.
	object := Type{ID: "github.com/weiloon1234/Foundry-Go/contract.CustomValueDTO", Kind: ObjectKind, Properties: []Property{
		{Name: "Code", Type: "github.com/weiloon1234/Foundry-Go/contract.TransportCode", Required: true},
	}}
	include := func() JSON[TransportCode] {
		return DefineJSONValue[TransportCode](Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.TransportCode", Types: []Type{
			{ID: "github.com/weiloon1234/Foundry-Go/contract.TransportCode", Kind: StringKind},
			{ID: "shared.int", Kind: IntegerKind, Signed: true, Bits: uint8(strconv.IntSize)},
		}})
	}
	shared := Schema{Root: object.ID, Types: []Type{object, JSONType("github.com/weiloon1234/Foundry-Go/contract.TransportCode", include), {ID: "shared.int", Kind: IntegerKind, Signed: true}}}
	if err := DefineJSON[CustomValueDTO](shared).Validate(); err != nil {
		t.Fatal("identical shared graph rejected", err)
	}
	shared.Types[2].Kind = StringKind
	shared.Types[2].Signed = false
	if DefineJSON[CustomValueDTO](shared).Validate() == nil {
		t.Fatal("conflicting graph accepted")
	}
	shared.Types[2] = shared.Types[0]
	if DefineJSON[CustomValueDTO](shared).Validate() == nil {
		t.Fatal("authored duplicate accepted")
	}
}

type IncompleteCode string

func (IncompleteCode) MarshalText() ([]byte, error) { return nil, nil }

type PointerCode string

func (*PointerCode) MarshalText() ([]byte, error) { return nil, nil }
func (*PointerCode) UnmarshalText([]byte) error   { return nil }

func TestCustomJSONValueRejectsIncompleteNativeMethods(t *testing.T) {
	for _, valid := range []error{
		DefineJSONValue[IncompleteCode](Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.IncompleteCode", Types: []Type{{ID: "github.com/weiloon1234/Foundry-Go/contract.IncompleteCode", Kind: StringKind}}}).Validate(),
		DefineJSONValue[PointerCode](Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.PointerCode", Types: []Type{{ID: "github.com/weiloon1234/Foundry-Go/contract.PointerCode", Kind: StringKind}}}).Validate(),
	} {
		if valid == nil {
			t.Fatal("incomplete/native-addressability contract accepted")
		}
	}
}

func TestCustomJSONValueConcurrentRequestsDoNotRerunFactory(t *testing.T) {
	calls := 0
	descriptor := customValueDTO(func() JSON[TransportCode] { calls++; return customCodeDescription() })
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			input, err := descriptor.Decode(context.Background(), []byte(`{"Code":"code_ok"}`), dtoLimits())
			if err != nil {
				t.Error(err)
				return
			}
			output, err := descriptor.Encode(context.Background(), input, dtoLimits())
			if err != nil || !json.Valid(output) {
				t.Error("encode", err)
			}
			description, err := descriptor.Description()
			if err != nil || reflect.DeepEqual(description, Schema{}) {
				t.Error("description", err)
			}
		})
	}
	workers.Wait()
	if calls != 1 {
		t.Fatal("factory shared request work", calls)
	}
}

// Native methods alone must not let a persistence model become a public value.
type CodecPersistence struct{}

func (CodecPersistence) MarshalText() ([]byte, error) { return []byte("private"), nil }
func (*CodecPersistence) UnmarshalText([]byte) error  { return nil }
func (CodecPersistence) FoundryIdentity() (model.Identity, error) {
	panic("identity methods must not run during contract construction")
}
func TestCustomJSONValueRejectsPersistenceIdentity(t *testing.T) {
	descriptor := DefineJSONValue[CodecPersistence](Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.CodecPersistence", Types: []Type{
		{ID: "github.com/weiloon1234/Foundry-Go/contract.CodecPersistence", Kind: StringKind},
	}})
	if descriptor.Validate() == nil {
		t.Fatal("persistence model became a custom response value")
	}
}
