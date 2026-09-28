package lifecycle_test

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestAssignmentAndStoredChangeAreIndependent(t *testing.T) {
	for _, test := range []struct {
		name          string
		before, after value.Optional[string]
		assigned      bool
		changed       bool
	}{
		{"normalized assignment", value.Set("ada@example.test"), value.Set("ada@example.test"), true, false},
		{"changed assignment", value.Set("before"), value.Set("after"), true, true},
		{"database changed value", value.Set("before"), value.Set("after"), false, true},
		{"unchanged omitted field", value.Set("same"), value.Set("same"), false, false},
		{"default on creation", value.Optional[string]{}, value.Set("default"), false, true},
		{"explicit zero on creation", value.Optional[string]{}, value.Set(""), true, true},
		{"deletion", value.Set("before"), value.Optional[string]{}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			change, err := lifecycle.CompareField(codec.String[string](), test.before, test.after, test.assigned)
			if err != nil || change.Before() != test.before || change.After() != test.after || change.Assigned() != test.assigned || change.Changed() != test.changed {
				t.Fatalf("incorrect field state: assigned=%v changed=%v err=%v", change.Assigned(), change.Changed(), err)
			}
		})
	}
}

func TestNullFieldIsDistinctFromMissingModel(t *testing.T) {
	absent := value.Optional[value.Nullable[int]]{}
	null := value.Set(value.Null[int]())
	zero := value.Set(value.Of(0))
	for _, test := range []struct {
		before, after value.Optional[value.Nullable[int]]
		changed       bool
	}{
		{absent, null, true}, {null, absent, true}, {null, null, false},
		{null, zero, true}, {zero, null, true}, {zero, zero, false},
	} {
		change, err := lifecycle.CompareField(codec.Nullable(codec.Signed[int]()), test.before, test.after, false)
		if err != nil || change.Changed() != test.changed || change.Before() != test.before || change.After() != test.after {
			t.Fatalf("lost NULL or model presence: %v", err)
		}
	}
}

func requireEqual[V any](t *testing.T, c codec.Codec[V], a, b V) {
	t.Helper()
	change, err := lifecycle.CompareField(c, value.Set(a), value.Set(b), true)
	if err != nil || change.Changed() || !change.Assigned() {
		t.Fatalf("equivalent codec values changed: %v", err)
	}
}

func TestCanonicalFieldComparisons(t *testing.T) {
	one, err := decimal.Parse("1.00")
	if err != nil {
		t.Fatal(err)
	}
	requireEqual(t, codec.Decimal(), one, decimal.FromInt64(1))
	a, err := value.ParseJSON[map[string]int](`{"b":2,"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := value.ParseJSON[map[string]int](`{"a":1,"b":2}`)
	if err != nil {
		t.Fatal(err)
	}
	requireEqual(t, codec.JSON[map[string]int](), a, b)
	instant := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	requireEqual(t, codec.Time(), instant, instant.In(time.FixedZone("consumer", 8*60*60)))
	requireEqual(t, codec.Float[float64](), float64(0), math.Copysign(0, -1))
	requireEqual(t, codec.Bool[bool](), false, false)
	change, err := lifecycle.CompareField(codec.Interval(), value.Set(temporal.Months(1)), value.Set(temporal.Days(30)), true)
	if err != nil || !change.Changed() {
		t.Fatalf("SQL interval comparison erased changed calendar components: %v", err)
	}
}

func TestInvalidChangesPublishNoSnapshots(t *testing.T) {
	for _, test := range []struct {
		before, after value.Optional[int]
		assigned      bool
	}{
		{value.Optional[int]{}, value.Optional[int]{}, false},
		{value.Set(1), value.Optional[int]{}, true},
	} {
		change, err := lifecycle.CompareField(codec.Signed[int](), test.before, test.after, test.assigned)
		if !errors.Is(err, fault.Invalid) || change.Before().IsSet() || change.After().IsSet() || change.Changed() || change.Assigned() {
			t.Fatalf("invalid change published state: %v", err)
		}
	}
	sentinel := errors.New("rejected snapshot")
	c := codec.New(func(v string) (driver.Value, error) {
		if v == "invalid" {
			return nil, sentinel
		}
		return v, nil
	}, func(v any) (string, error) { return v.(string), nil })
	for _, pair := range [][2]string{{"invalid", "valid"}, {"valid", "invalid"}} {
		change, err := lifecycle.CompareField(c, value.Set(pair[0]), value.Set(pair[1]), true)
		if !errors.Is(err, sentinel) || change.Before().IsSet() || change.After().IsSet() {
			t.Fatalf("failed comparison lost its cause or retained snapshots: %v", err)
		}
	}
	if _, err := lifecycle.CompareField(codec.Codec[int]{}, value.Set(1), value.Set(1), true); !errors.Is(err, fault.Invalid) {
		t.Fatalf("invalid codec accepted: %v", err)
	}
}

func TestChangeDiagnosticsDoNotFormatSnapshots(t *testing.T) {
	change, err := lifecycle.CompareField(codec.String[string](), value.Set("private-before"), value.Set("private-after"), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, formatted := range []string{fmt.Sprint(change), fmt.Sprintf("%+v", change), fmt.Sprintf("%#v", change)} {
		if strings.Contains(formatted, "private-") {
			t.Fatal("routine change formatting exposed field values")
		}
	}
	if got, present := change.After().Get(); !present || got != "private-after" {
		t.Fatal("explicit typed snapshot access was lost")
	}
}

func TestCustomCodecRepresentationsPreserveByteAndNumberStates(t *testing.T) {
	c := codec.New(func(v string) (driver.Value, error) {
		switch v {
		case "nil buffer":
			return []byte(nil), nil
		case "empty buffer":
			return []byte{}, nil
		case "scalar":
			return "same", nil
		default:
			return []byte("same"), nil
		}
	}, func(any) (string, error) { return "", nil })
	for _, test := range []struct {
		before, after string
		changed       bool
	}{
		{"nil buffer", "empty buffer", true}, {"empty buffer", "nil buffer", true},
		{"nil buffer", "nil buffer", false}, {"empty buffer", "empty buffer", false},
		{"bytes", "other bytes", false}, {"scalar", "bytes", true}, {"bytes", "scalar", true},
	} {
		change, err := lifecycle.CompareField(c, value.Set(test.before), value.Set(test.after), true)
		if err != nil || change.Changed() != test.changed {
			t.Fatalf("custom representation comparison failed: %v", err)
		}
	}
	number := codec.New(func(v float64) (driver.Value, error) { return v, nil }, func(v any) (float64, error) { return v.(float64), nil })
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, pair := range [][2]float64{{invalid, 0}, {0, invalid}} {
			change, err := lifecycle.CompareField(number, value.Set(pair[0]), value.Set(pair[1]), true)
			if !errors.Is(err, fault.Invalid) || change.Before().IsSet() || change.After().IsSet() {
				t.Fatalf("non-finite representation published change data: %v", err)
			}
		}
	}
}
