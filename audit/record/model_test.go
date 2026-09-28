package record_test

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestAuditRejectsIdentityLeaksMismatchesAndPartialCapture(t *testing.T) {
	called := false
	secret := codec.New(func(string) (driver.Value, error) { called = true; return "private", nil }, func(any) (string, error) { return "private", nil }).WithParameterType(codec.TypeText)
	ref := model.NewReference[subject]("audited_subjects", "private", secret)
	for _, test := range []struct {
		name       string
		disclosure record.Disclosure
	}{
		{"access_token", record.Automatic}, {"id", record.Exclude}, {"id", record.Redact},
	} {
		if _, err := record.NewBuilder(ref, lifecycle.Create, test.name, test.disclosure); !errors.Is(err, fault.Invalid) {
			t.Fatal("unsafe subject policy was accepted", err)
		}
	}
	if called {
		t.Fatal("rejected subject policy encoded its secret key")
	}
	c := codec.Signed[int64]()
	b, err := record.NewBuilder(model.NewReference[subject]("audited_subjects", int64(7), c), lifecycle.Create, "id", record.Automatic)
	if err != nil {
		t.Fatal(err)
	}
	wrong := capture(t, "id", c, changed(t, c, value.Optional[int64]{}, value.Set(int64(8)), true), record.Automatic)
	if err := b.Add(wrong); !errors.Is(err, fault.Invalid) {
		t.Fatal("audit subject mismatch was accepted", err)
	}
	if _, err := b.Build(); !errors.Is(err, fault.Invalid) {
		t.Fatal("ignored capture error published a partial record", err)
	}
	good := builder(t, lifecycle.Create)
	item := built(t, good)
	if _, err := good.Build(); !errors.Is(err, fault.Closed) {
		t.Fatal("completed builder was reused", err)
	}
	if err := good.Add(wrong); !errors.Is(err, fault.Closed) {
		t.Fatal("completed record accepted later fields", err)
	}
	other := model.NewReference[subject]("different_subjects", int64(0), c)
	if _, err := record.RestoreModel(other, item.Entry()); !errors.Is(err, fault.Invalid) {
		t.Fatal("typed restore ignored the stored model namespace", err)
	}
	if _, err := record.NewBuilder(model.NewReference[subject]("audited_subjects", int64(7), c), lifecycle.Operation(0), "id", record.Automatic); !errors.Is(err, fault.Invalid) {
		t.Fatal("standalone comparison became a lifecycle operation", err)
	}
}

func TestAuditBoundsTheWholeRecordAndRejectsDuplicateColumns(t *testing.T) {
	c := codec.String[string]()
	large := strings.Repeat("x", value.JSONMaxBytes/2)
	change := changed(t, c, value.Optional[string]{}, value.Set(large), true)
	b := builder(t, lifecycle.Create)
	if err := b.Add(capture(t, "first", c, change, record.Automatic)); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(capture(t, "second", c, change, record.Automatic)); !errors.Is(err, fault.Invalid) {
		t.Fatal("aggregate audit payload exceeded its bound", err)
	}
	if _, err := b.Build(); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized capture published a partial record", err)
	}
	duplicate := builder(t, lifecycle.Create)
	small := capture(t, "label", c, changed(t, c, value.Optional[string]{}, value.Set("small"), true), record.Exclude)
	if err := duplicate.Add(small); err != nil {
		t.Fatal(err)
	}
	if err := duplicate.Add(small); !errors.Is(err, fault.Duplicate) {
		t.Fatal("excluded columns bypassed duplicate detection", err)
	}
}

func TestAuditKeepsDecimalsIntervalsAndInstantsThroughTheirCodecs(t *testing.T) {
	amount, err := decimal.Parse("9007199254740993.0012")
	if err != nil {
		t.Fatal(err)
	}
	b := builder(t, lifecycle.Update)
	decimalCodec := codec.Decimal()
	if err := b.Add(capture(t, "balance", decimalCodec, changed(t, decimalCodec, value.Set(amount), value.Set(amount), true), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	intervalCodec := codec.Interval()
	if err := b.Add(capture(t, "duration", intervalCodec, changed(t, intervalCodec, value.Set(temporal.Months(1)), value.Set(temporal.Days(30)), true), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 9, 13, 8, 9, 10, 123000000, time.UTC)
	instantCodec := codec.Time()
	if err := b.Add(capture(t, "at", instantCodec, changed(t, instantCodec, value.Set(instant.In(time.FixedZone("fixture", 8*60*60))), value.Set(instant), true), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	view := inspect(t, built(t, b))
	decimalField, err := record.ReadField(view, "balance", decimalCodec)
	if err != nil {
		t.Fatal(err)
	}
	decimalChange, _ := decimalField.Get()
	decimalResult, err := decimalChange.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	decodedDecimal, present := decimalResult.Get()
	if !present || decodedDecimal != amount || decimalChange.Changed() || !decimalChange.Assigned() {
		t.Fatal("exact decimal or dirty flags changed")
	}
	intervalField, err := record.ReadField(view, "duration", intervalCodec)
	if err != nil {
		t.Fatal(err)
	}
	intervalChange, _ := intervalField.Get()
	before, err := intervalChange.Before().Get()
	if err != nil {
		t.Fatal(err)
	}
	after, err := intervalChange.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	left, _ := before.Get()
	right, _ := after.Get()
	if left != temporal.Months(1) || right != temporal.Days(30) || !intervalChange.Changed() {
		t.Fatal("calendar months collapsed into SQL-equivalent days")
	}
	timeField, err := record.ReadField(view, "at", instantCodec)
	if err != nil {
		t.Fatal(err)
	}
	timeChange, _ := timeField.Get()
	decodedTime, err := timeChange.Before().Get()
	if err != nil {
		t.Fatal(err)
	}
	storedTime, present := decodedTime.Get()
	if !present || !storedTime.Equal(instant) || timeChange.Changed() {
		t.Fatal("stored instant changed with presentation timezone")
	}
}

type buffer struct{ data []byte }

func TestAuditCapturesAndDecodesOwnedByteValues(t *testing.T) {
	c := codec.New(func(v buffer) (driver.Value, error) { return v.data, nil }, func(raw any) (buffer, error) {
		data, ok := raw.([]byte)
		if !ok {
			return buffer{}, errors.New("expected bytes")
		}
		return buffer{data: data}, nil
	}).WithParameterType(codec.TypeBytes)
	input := buffer{data: []byte("captured")}
	change := changed(t, c, value.Optional[buffer]{}, value.Set(input), true)
	field := capture(t, "data", c, change, record.Automatic)
	input.data[0] = 'X'
	b := builder(t, lifecycle.Create)
	if err := b.Add(field); err != nil {
		t.Fatal(err)
	}
	view := inspect(t, built(t, b))
	read, err := record.ReadField(view, "data", c)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := read.Get()
	first, err := result.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	firstValue, _ := first.Get()
	if string(firstValue.data) != "captured" {
		t.Fatal("capture retained the caller's bytes")
	}
	firstValue.data[0] = 'Y'
	second, err := result.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	secondValue, _ := second.Get()
	if string(secondValue.data) != "captured" {
		t.Fatal("decoding shared a mutable byte buffer")
	}
}

func TestSensitiveNameConventionAndObserverIdentity(t *testing.T) {
	for _, name := range []string{"password_hash", "accessToken", "APIKey", "private-key", "authorization", "credentials", "nested_secret_value"} {
		if !record.SensitiveName(name) {
			t.Fatal("sensitive name was missed", name)
		}
	}
	for _, name := range []string{"id", "email", "tokenizer", "compass", "private_notes", "api_version"} {
		if record.SensitiveName(name) {
			t.Fatal("unrelated name was redacted", name)
		}
	}
	first, err := record.ObserverName[subject]()
	if err != nil {
		t.Fatal(err)
	}
	second, err := record.ObserverName[buffer]()
	if err != nil {
		t.Fatal(err)
	}
	again, err := record.ObserverName[subject]()
	if err != nil {
		t.Fatal(err)
	}
	if first != again || first == second || len(first) > 128 {
		t.Fatal("audit observer identifiers are unstable or ambiguous")
	}
	if _, err := record.ObserverName[struct{}](); !errors.Is(err, fault.Invalid) {
		t.Fatal("anonymous model received a shared audit observer name", err)
	}
}
