package model_test

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

type referencedMember struct{}
type referencedTeam struct{}
type naturalCode string

func (naturalCode) MarshalJSON() ([]byte, error) {
	panic("presentation must never run for a stored key")
}

func TestReferencePreservesTypedKeysAndPersistenceEncoding(t *testing.T) {
	ref := model.NewReference[referencedMember]("members", int64(math.MaxInt64), codec.Signed[int64]())
	identity, err := ref.Identity()
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.KeyJSON()
	if err != nil || key != `{"k":"int","t":"9223372036854775807"}` {
		t.Fatal("numeric identity lost precision", err)
	}
	decoded, err := ref.Parse(identity)
	if err != nil || decoded.Key() != ref.Key() || decoded.ModelName() != "members" {
		t.Fatal("typed reference round trip failed", err)
	}
	if _, err := model.NewReference[referencedTeam]("teams", int64(0), codec.Signed[int64]()).Parse(identity); !errors.Is(err, fault.Invalid) {
		t.Fatal("cross-model identity accepted", err)
	}
	if _, err := model.NewReference[referencedMember]("members", "", codec.String[string]()).Parse(identity); !errors.Is(err, fault.Invalid) {
		t.Fatal("numeric identity became a string", err)
	}
	textNumber, err := model.NewReference[referencedMember]("members", "7", codec.String[string]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ref.Parse(textNumber); !errors.Is(err, fault.Invalid) {
		t.Fatal("text identity silently became an integer", err)
	}
	naturalRef := model.NewReference[referencedMember]("members", naturalCode(" exact "), codec.String[naturalCode]())
	natural, err := naturalRef.Identity()
	if err != nil {
		t.Fatal(err)
	}
	text, err := natural.KeyJSON()
	if err != nil || text != `{"k":"string","t":" exact "}` {
		t.Fatal("natural key was normalized or used its presentation marshaler", err)
	}
	parsed, err := naturalRef.Parse(natural)
	if err != nil || parsed.Key() != naturalRef.Key() {
		t.Fatal("named key codec failed", err)
	}
	data, err := json.Marshal(natural)
	if err != nil {
		t.Fatal(err)
	}
	var restored model.Identity
	if err := json.Unmarshal(data, &restored); err != nil || restored != natural {
		t.Fatal("identity envelope round trip failed", err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if fmt.Sprintf(format, ref) != "model reference" || fmt.Sprintf(format, natural) != "model identity" {
			t.Fatal("routine formatting exposed a key")
		}
	}
}

func TestReferenceSnapshotsOwnedBytesAndRejectsSQLNull(t *testing.T) {
	bytesCodec := codec.New(func(v []byte) (driver.Value, error) { return v, nil }, func(v any) ([]byte, error) {
		b, ok := v.([]byte)
		if !ok {
			return nil, fault.New(fault.Invalid, "expected bytes")
		}
		return slices.Clone(b), nil
	})
	input := []byte{0, 1, 2, 255}
	ref := model.NewReference[referencedMember]("members", input, bytesCodec)
	identity, err := ref.Identity()
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 99
	parsed, err := ref.Parse(identity)
	if err != nil || !slices.Equal(parsed.Key(), []byte{0, 1, 2, 255}) {
		t.Fatal("identity retained mutable key storage", err)
	}
	parsed.Key()[0] = 88
	again, err := ref.Parse(identity)
	if err != nil || again.Key()[0] != 0 {
		t.Fatal("identity decode reused byte storage", err)
	}
	nullCodec := codec.New(func(int) (driver.Value, error) { return nil, nil }, func(any) (int, error) { return 0, nil })
	if _, err := model.NewReference[referencedMember]("members", 0, nullCodec).Identity(); !errors.Is(err, fault.Invalid) {
		t.Fatal("SQL NULL accepted as identity", err)
	}
}

func TestReferenceRejectsInvalidIdentityBoundariesWithoutChangingDestination(t *testing.T) {
	valid, err := model.ParseIdentity("members", `{"k":"string","t":"kept"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"model":"members","key":null}`, `{"model":"members","key":{}}`,
		`{"model":"members","key":[]}`, `{"model":"members","key":{"k":"int","t":"7","t":"8"}}`,
		`{"model":"members","key":{"k":"int","t":"7"},"extra":1}`, `{"model":"bad table","key":{"k":"int","t":"7"}}`,
		`{"model":"members","key":{"k":"null"}}`, `{"model":"members","key":{"k":"unknown"}}`,
		`{"model":"members","key":{"k":"int","t":"9223372036854775808"}}`, `{"model":"members","key":{"k":"float","t":"NaN"}}`,
		`{"model":"members","key":{"k":"bytes","t":"***"}}`, `{"model":"members","key":{"k":"int","t":"7","extra":true}}`,
		`{"model":"members"}`, `null`,
	} {
		destination := valid
		if err := json.Unmarshal([]byte(raw), &destination); err == nil || destination != valid {
			t.Fatal("invalid identity replaced a valid destination", raw, err)
		}
	}
	if _, err := model.NewReference[referencedMember]("members", strings.Repeat("x", model.MaxIdentityKeyBytes), codec.String[string]()).Identity(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded encoded identity accepted", err)
	}
	dynamic := codec.New(func(any) (driver.Value, error) { return "dynamic", nil }, func(any) (any, error) { return "dynamic", nil })
	if _, err := model.NewReference[referencedMember, any]("members", "dynamic", dynamic).Identity(); !errors.Is(err, fault.Invalid) {
		t.Fatal("dynamic key accepted", err)
	}
	if _, err := (model.Reference[referencedMember, int]{}).Identity(); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero reference accepted", err)
	}
	if _, err := (model.Identity{}).KeyJSON(); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty identity accepted", err)
	}
	var destination *model.Identity
	if err := destination.UnmarshalJSON([]byte(`{}`)); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil identity destination accepted", err)
	}
}

func TestReferenceIsolatesCustomCodecFailures(t *testing.T) {
	sentinel := errors.New("private identity codec failure")
	for _, decode := range []bool{false, true} {
		for _, failure := range []string{"panic", "exit", "error"} {
			fail := func() error {
				switch failure {
				case "panic":
					panic("private identity payload")
				case "exit":
					runtime.Goexit()
				}
				return sentinel
			}
			c := codec.New(func(v int) (driver.Value, error) {
				if !decode {
					return nil, fail()
				}
				return int64(v), nil
			}, func(any) (int, error) { return 0, fail() })
			ref := model.NewReference[referencedMember]("members", 7, c)
			identity, err := ref.Identity()
			if decode {
				if err != nil {
					t.Fatal(err)
				}
				_, err = ref.Parse(identity)
			}
			expected := error(fault.Panicked)
			if failure == "error" {
				expected = sentinel
			}
			if !errors.Is(err, expected) || strings.Contains(fmt.Sprintf("%v %#v", err, err), "private") {
				t.Fatal("identity callback failure escaped or leaked", decode, failure, err)
			}
		}
	}
}
