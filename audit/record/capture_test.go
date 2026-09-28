package record_test

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type subject struct{}

func changed[V any](t *testing.T, c codec.Codec[V], before, after value.Optional[V], assigned bool) lifecycle.FieldChange[V] {
	t.Helper()
	change, err := lifecycle.CompareField(c, before, after, assigned)
	if err != nil {
		t.Fatal(err)
	}
	return change
}

func capture[V any](t *testing.T, name string, c codec.Codec[V], change lifecycle.FieldChange[V], disclosure record.Disclosure) record.Field[subject] {
	t.Helper()
	field, err := record.CaptureField[subject](name, c, change, disclosure)
	if err != nil {
		t.Fatal(err)
	}
	return field
}

func builder(t *testing.T, operation lifecycle.Operation) *record.Builder[subject, int64] {
	t.Helper()
	c := codec.Signed[int64]()
	b, err := record.NewBuilder(model.NewReference[subject]("audited_subjects", int64(7), c), operation, "id", record.Automatic)
	if err != nil {
		t.Fatal(err)
	}
	before, after := value.Set(int64(7)), value.Set(int64(7))
	if operation == lifecycle.Create {
		before = value.Optional[int64]{}
	}
	if operation == lifecycle.Delete || operation == lifecycle.ForceDelete {
		after = value.Optional[int64]{}
	}
	if err := b.Add(capture(t, "id", c, changed(t, c, before, after, operation == lifecycle.Create), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	return b
}

func built(t *testing.T, b *record.Builder[subject, int64]) record.Model[subject, int64] {
	t.Helper()
	item, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func inspect(t *testing.T, item record.Model[subject, int64]) record.View[subject, int64] {
	t.Helper()
	view, err := record.Inspect(item)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestAuditKeepsAssignedUnchangedAndExplicitNullStates(t *testing.T) {
	b := builder(t, lifecycle.Update)
	text := codec.String[string]()
	if err := b.Add(capture(t, "email", text, changed(t, text, value.Set("stored@example.test"), value.Set("stored@example.test"), true), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	nullable := codec.Nullable(codec.Signed[int]())
	if err := b.Add(capture(t, "score", nullable, changed(t, nullable, value.Set(value.Of(0)), value.Set(value.Null[int]()), true), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	item := built(t, b)
	ref, err := item.Subject()
	if err != nil || ref.Key() != 7 || item.Operation() != lifecycle.Update {
		t.Fatal("captured subject or operation changed", err)
	}
	view := inspect(t, item)
	email, err := record.ReadField(view, "email", text)
	if err != nil {
		t.Fatal(err)
	}
	field, present := email.Get()
	if !present || !field.Assigned() || field.Changed() {
		t.Fatal("assignment was confused with a stored change")
	}
	result, err := field.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	stored, present := result.Get()
	if !present || stored != "stored@example.test" {
		t.Fatal("audit did not retain its stored field")
	}
	score, err := record.ReadField(view, "score", nullable)
	if err != nil {
		t.Fatal(err)
	}
	scoreField, present := score.Get()
	if !present || !scoreField.Changed() {
		t.Fatal("NULL transition was lost")
	}
	after, err := scoreField.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	valueAfter, present := after.Get()
	if !present || !valueAfter.IsNull() || scoreField.After().State() != record.Disclosed {
		t.Fatal("SQL NULL became absent model or scalar zero")
	}
	created := builder(t, lifecycle.Create)
	if err := created.Add(capture(t, "score", nullable, changed(t, nullable, value.Optional[value.Nullable[int]]{}, value.Set(value.Null[int]()), false), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	createdFields := inspect(t, built(t, created))
	newScore, err := record.ReadField(createdFields, "score", nullable)
	if err != nil {
		t.Fatal(err)
	}
	newField, _ := newScore.Get()
	if newField.Before().State() != record.Absent || newField.After().State() != record.Disclosed {
		t.Fatal("creation lost model absence or field NULL")
	}
}

func TestAuditRedactsAndExcludesBeforeInvokingFieldCodecs(t *testing.T) {
	b := builder(t, lifecycle.Update)
	plain := codec.String[string]()
	change := changed(t, plain, value.Set("old-private-value"), value.Set("new-private-value"), true)
	called := 0
	forbidden := codec.New(func(string) (driver.Value, error) { called++; panic("private encoder") }, func(any) (string, error) { panic("private decoder") }).WithParameterType(codec.TypeText)
	if err := b.Add(capture(t, "password_hash", forbidden, change, record.Automatic)); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(capture(t, "private_notes", forbidden, change, record.Exclude)); err != nil {
		t.Fatal(err)
	}
	item := built(t, b)
	if called != 0 {
		t.Fatal("sensitive/excluded audit value invoked its codec")
	}
	payload, err := item.Entry().Payload()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "private-value") || strings.Contains(payload, "private_notes") {
		t.Fatal("excluded or sensitive data reached storage")
	}
	view := inspect(t, item)
	password, err := record.ReadField(view, "password_hash", plain)
	if err != nil {
		t.Fatal(err)
	}
	field, present := password.Get()
	if !present || field.Before().State() != record.Redacted || field.After().State() != record.Redacted || !field.Changed() {
		t.Fatal("redaction lost its explicit state or change flag")
	}
	if _, err := field.After().Get(); !errors.Is(err, fault.Missing) {
		t.Fatal("redaction fabricated a typed field", err)
	}
	excluded, err := record.ReadField(view, "private_notes", plain)
	if err != nil || excluded.IsSet() {
		t.Fatal("excluded field remained visible", err)
	}
	for _, object := range []any{b, item, item.Entry(), view, field, field.After(), field.After().Snapshot()} {
		if output := fmt.Sprintf("%+v %#v", object, object); strings.Contains(output, "private-value") {
			t.Fatal("audit diagnostics exposed contents")
		}
	}
}

func TestAuditJSONRedactionRetainsExactNumbersAndRejectsTamperedMarkers(t *testing.T) {
	input, err := value.ParseJSON[json.RawMessage](`{"amount":9007199254740993.001200,"profile":{"apiToken":"payload-secret","public":42},"rows":[{"private_key":"payload-secret"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	c := codec.JSON[json.RawMessage]()
	b := builder(t, lifecycle.Create)
	if err := b.Add(capture(t, "metadata", c, changed(t, c, value.Optional[value.JSON[json.RawMessage]]{}, value.Set(input), true), record.Automatic)); err != nil {
		t.Fatal(err)
	}
	item := built(t, b)
	payload, err := item.Entry().Payload()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "payload-secret") || !strings.Contains(payload, "9007199254740993.0012") || !strings.Contains(payload, "[redacted]") {
		t.Fatal("JSON redaction leaked a secret or rounded a number")
	}
	view := inspect(t, item)
	metadata, err := record.ReadField(view, "metadata", c)
	if err != nil {
		t.Fatal(err)
	}
	field, present := metadata.Get()
	if !present || field.After().State() != record.RedactedJSON {
		t.Fatal("nested redaction lost its explicit state")
	}
	if _, err := field.After().Get(); !errors.Is(err, fault.Missing) {
		t.Fatal("partly redacted JSON became an original model field", err)
	}
	for _, corrupt := range []string{
		strings.ReplaceAll(payload, "[redacted]", "payload-secret"),
		strings.ReplaceAll(payload, `"state":3`, `"state":1`),
	} {
		if _, err := record.ParseEntry(item.Entry().Identity(), item.Operation(), corrupt); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid persisted redaction was accepted", err)
		}
	}
}

func TestAuditContainsCustomCodecFailuresWithoutExposingTheirMessages(t *testing.T) {
	change := changed(t, codec.String[string](), value.Optional[string]{}, value.Set("stored"), true)
	private := errors.New("private codec contents")
	for _, mode := range []string{"panic", "exit", "error"} {
		t.Run(mode, func(t *testing.T) {
			c := codec.New(func(string) (driver.Value, error) {
				switch mode {
				case "panic":
					panic(private)
				case "exit":
					runtime.Goexit()
				}
				return nil, private
			}, func(any) (string, error) { return "", nil }).WithParameterType(codec.TypeText)
			_, err := record.CaptureField[subject]("value", c, change, record.Automatic)
			if !errors.Is(err, fault.Invalid) || strings.Contains(fmt.Sprintf("%+v %#v", err, err), "private") {
				t.Fatal("codec failure escaped or exposed data")
			}
			if mode == "error" && !errors.Is(err, private) {
				t.Fatal("safe error wrapper lost its cause")
			}
			if mode != "error" && !errors.Is(err, fault.Panicked) {
				t.Fatal("custom callback exit lost its classification", err)
			}
		})
	}
}

func TestSensitiveCodecRedactsAnOrdinaryColumnName(t *testing.T) {
	c := codec.String[string]().WithSensitiveValues()
	b := builder(t, lifecycle.Update)
	change := changed(t, c, value.Set("private-old"), value.Set("private-new"), true)
	if err := b.Add(capture(t, "digest", c, change, record.Automatic)); err != nil {
		t.Fatal(err)
	}
	item := built(t, b)
	field, err := record.ReadField(inspect(t, item), "digest", c)
	if err != nil {
		t.Fatal(err)
	}
	got, present := field.Get()
	if !present {
		t.Fatal("redaction discarded change flags")
	}
	if _, err := got.After().Get(); err == nil {
		t.Fatal("sensitive codec bypassed redaction")
	}
	encoded, err := json.Marshal(item.Entry())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-old") || strings.Contains(string(encoded), "private-new") {
		t.Fatal("audit leaked sensitive persistence value")
	}
}
