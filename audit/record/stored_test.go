package record_test

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestStoredCaptureRejectsZeroSealAndRedactsBeforeBinding(t *testing.T) {
	b := builder(t, lifecycle.Create)
	calls := 0
	f := record.FoundryStoredField{Name: "private", Type: codec.TypeText, After: true, Changed: true, Bind: func(bool) (driver.Value, error) { calls++; return "secret value", nil }}
	if err := record.FoundryCapture(b, f, record.Automatic, sqlowner.MetadataSeal{}); !errors.Is(err, fault.Invalid) || calls != 0 {
		t.Fatal("zero capability invoked callback", err, calls)
	}
	f.Sensitive = true
	if err := record.FoundryCapture(b, f, record.Automatic, sqlowner.Metadata()); err != nil || calls != 0 {
		t.Fatal("sensitive callback bound", err, calls)
	}
	history := built(t, b)
	view := inspect(t, history)
	item, err := record.ReadField(view, "private", codec.String[string]())
	if err != nil {
		t.Fatal(err)
	}
	field, _ := item.Get()
	if field.After().State() != record.Redacted {
		t.Fatal("sensitive value disclosed")
	}
}

func TestHistoricalDecoderBridgeRejectsZeroSeal(t *testing.T) {
	b := builder(t, lifecycle.Create)
	view := inspect(t, built(t, b))
	c := codec.Signed[int64]()
	calls := 0
	decode := func(v any) (int64, error) { calls++; return c.Decode(v) }
	if _, err := record.FoundryReadField(view, "id", c.ParameterType(), decode, sqlowner.MetadataSeal{}); !errors.Is(err, fault.Invalid) || calls != 0 {
		t.Fatal("zero capability accepted", err)
	}
	field, err := record.FoundryReadField(view, "id", c.ParameterType(), decode, sqlowner.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	f, _ := field.Get()
	v, err := f.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := v.Get()
	if key != 7 || calls != 1 {
		t.Fatal("declared decoder was not used")
	}
}
