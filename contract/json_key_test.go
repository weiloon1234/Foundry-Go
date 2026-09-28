package contract

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type KeyedDTO struct{ Values map[int8]string }

func integerKeyDTO() JSON[KeyedDTO] {
	return DefineJSON[KeyedDTO](Schema{
		Root: "github.com/weiloon1234/Foundry-Go/contract.KeyedDTO", Types: []Type{
			{ID: "github.com/weiloon1234/Foundry-Go/contract.KeyedDTO", Kind: ObjectKind, Properties: []Property{{Name: "Values", Type: "values", Required: true}}},
			JSONMapType[map[int8]string]("values", "text", "int8", IntegerJSONKey[int8]()),
			{ID: "text", Kind: StringKind},
		},
	})
}

func TestTypedJSONMapKeyMetadataAndStrictDecode(t *testing.T) {
	descriptor := integerKeyDTO()
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	schema, err := descriptor.Description()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range schema.Types {
		typ := &schema.Types[i]
		if typ.ID == "values" {
			found = true
			if typ.Key == nil || typ.Key.Value.Kind != IntegerKind || typ.Key.Value.Bits != 8 || !typ.Key.Value.Signed || typ.Key.ServerOnly {
				t.Fatal("key shape missing")
			}
			typ.Key.Value.Bits = 64
		}
	}
	if !found {
		t.Fatal("missing map type")
	}
	again, _ := descriptor.Description()
	for _, typ := range again.Types {
		if typ.ID == "values" && typ.Key.Value.Bits != 8 {
			t.Fatal("key metadata shared")
		}
	}
	input, err := descriptor.Decode(t.Context(), []byte(`{"Values":{"-128":"low","127":"high"}}`), dtoLimits())
	if err != nil || input.Values[-128] != "low" {
		t.Fatal("typed key decode", err)
	}
	output, err := descriptor.Encode(t.Context(), input, dtoLimits())
	if err != nil || !json.Valid(output) {
		t.Fatal("typed key encode", err)
	}
	for _, text := range []string{`{"Values":{"01":"x","1":"y"}}`, `{"Values":{"128":"x"}}`, `{"Values":{"-0":"x"}}`} {
		input, err := descriptor.Decode(t.Context(), []byte(text), dtoLimits())
		var rejected *DecodeError
		if !errors.As(err, &rejected) || input.Values != nil {
			t.Fatal("invalid or partial map returned", err)
		}
		issues := rejected.Issues()
		if len(issues) != 1 || issues[0].Path != "/Values" || issues[0].Code != KeyIssue {
			t.Fatal("key diagnostics leaked names", issues)
		}
	}
}

type KeyStatus string

func (k KeyStatus) MarshalText() ([]byte, error)     { return []byte(k), nil }
func (k *KeyStatus) UnmarshalText(data []byte) error { *k = KeyStatus(data); return nil }

type StatusMapDTO struct{ Values map[KeyStatus]string }

func TestJSONEnumKeyMembershipAndDescriptorOwnership(t *testing.T) {
	descriptor := enum.Describe("app", "KeyStatus", enum.Case[KeyStatus]{Name: "Ready", Value: "ready"})
	key := EnumJSONKey(descriptor)
	info, err := key.Description()
	if err != nil || len(info.Value.Cases) != 1 {
		t.Fatal("enum key metadata", err)
	}
	info.Value.Cases[0][1] = 'x'
	next, _ := key.Description()
	if string(next.Value.Cases[0]) != `"ready"` {
		t.Fatal("enum bytes shared")
	}
	schema := Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.StatusMapDTO", Types: []Type{
		{ID: "github.com/weiloon1234/Foundry-Go/contract.StatusMapDTO", Kind: ObjectKind, Properties: []Property{{Name: "Values", Type: "values", Required: true}}},
		JSONMapType[map[KeyStatus]string]("values", "text", "app.KeyStatus", key),
		{ID: "text", Kind: StringKind},
	}}
	response := DefineJSON[StatusMapDTO](schema)
	if _, err := response.Decode(t.Context(), []byte(`{"Values":{"unknown":"x"}}`), dtoLimits()); err == nil {
		t.Fatal("enum membership not checked")
	}
	if _, err := response.Decode(t.Context(), []byte(`{"Values":{"ready":"x"}}`), dtoLimits()); err != nil {
		t.Fatal(err)
	}
	schema.Types[1].Key.Value.Cases[0][1] = 'x'
	if DefineJSON[StatusMapDTO](schema).Validate() == nil {
		t.Fatal("key metadata detached from runtime")
	}
}

type FailingKey string

func (k *FailingKey) UnmarshalText(data []byte) error {
	if string(data) == "panic" {
		panic("private key panic")
	}
	if string(data) == "goexit" {
		runtime.Goexit()
	}
	*k = FailingKey(data)
	return nil
}

type FailingKeyDTO struct{ Values map[FailingKey]string }

func TestJSONMapKeyPanicAndGoexitAreInternal(t *testing.T) {
	descriptor := DefineJSON[FailingKeyDTO](Schema{Root: "github.com/weiloon1234/Foundry-Go/contract.FailingKeyDTO", Types: []Type{
		{ID: "github.com/weiloon1234/Foundry-Go/contract.FailingKeyDTO", Kind: ObjectKind, Properties: []Property{{Name: "Values", Type: "values", Required: true}}},
		JSONMapType[map[FailingKey]string]("values", "text", "app.FailingKey", StringJSONKey[FailingKey]()),
		{ID: "text", Kind: StringKind},
	}})
	for _, name := range []string{"panic", "goexit"} {
		result, err := descriptor.Decode(context.Background(), []byte("{\"Values\":{\""+name+"\":\"x\"}}"), dtoLimits())
		if !errors.Is(err, fault.Internal) || result.Values != nil || strings.Contains(err.Error(), "private") {
			t.Fatal("key panic became input rejection", err)
		}
	}
	for name, factory := range map[string]func() JSONKey[string]{
		"nil":    nil,
		"panic":  func() JSONKey[string] { panic("private") },
		"goexit": func() JSONKey[string] { runtime.Goexit(); return JSONKey[string]{} },
		"zero":   func() JSONKey[string] { return JSONKey[string]{} },
	} {
		if ResolveJSONKey(factory).Validate() == nil {
			t.Fatal("key factory accepted", name)
		}
	}
}

type ObservedKeyValue string

var keyValueDecodes int

func (v ObservedKeyValue) MarshalText() ([]byte, error) { return []byte(v), nil }
func (v *ObservedKeyValue) UnmarshalText(data []byte) error {
	keyValueDecodes++
	*v = ObservedKeyValue(data)
	return nil
}

type ObservedKeyDTO struct{ Values map[int8]ObservedKeyValue }

func TestJSONMapKeysValidateBeforeValueHydration(t *testing.T) {
	valueID := TypeID("github.com/weiloon1234/Foundry-Go/contract.ObservedKeyValue")
	rootID := TypeID("github.com/weiloon1234/Foundry-Go/contract.ObservedKeyDTO")
	d := DefineJSON[ObservedKeyDTO](Schema{Root: rootID, Types: []Type{
		{ID: rootID, Kind: ObjectKind, Properties: []Property{{Name: "Values", Type: "map", Required: true}}},
		JSONMapType[map[int8]ObservedKeyValue]("map", valueID, "int8", IntegerJSONKey[int8]()),
		JSONType[ObservedKeyValue](valueID, func() JSON[ObservedKeyValue] {
			return ScalarJSON(DefineScalar[ObservedKeyValue](Type{ID: valueID, Kind: StringKind}))
		}),
	}})
	keyValueDecodes = 0
	result, err := d.Decode(t.Context(), []byte(`{"Values":{"1":"safe","01":"alias"}}`), dtoLimits())
	if err == nil || result.Values != nil || keyValueDecodes != 0 {
		t.Fatal("value hydration preceded key rejection", err, keyValueDecodes)
	}
	if _, err := d.Decode(t.Context(), []byte(`{"Values":{"1":"safe"}}`), dtoLimits()); err != nil || keyValueDecodes != 1 {
		t.Fatal("valid native value was not hydrated", err, keyValueDecodes)
	}
}

type FirstMapKey string
type SecondMapKey string

func TestJSONMapKeyRuntimeOwnershipInIncludedSchemas(t *testing.T) {
	first := JSONMapType[map[FirstMapKey]string]("map", "string", "key", StringJSONKey[FirstMapKey]())
	same := JSONMapType[map[FirstMapKey]string]("map", "string", "key", StringJSONKey[FirstMapKey]())
	other := JSONMapType[map[SecondMapKey]string]("map", "string", "key", StringJSONKey[SecondMapKey]())
	if !sameNormalizedType(first, same) || sameNormalizedType(first, other) {
		t.Fatal("matching metadata erased concrete native key ownership")
	}
}
