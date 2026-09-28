package query

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCursorInputValidationPreservesQueryOwnership(t *testing.T) {
	first, err := cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	token, err := makeCursor(first.scope, first.fields, cursorRecord{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := token.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, request := range []CursorRequest[cursorRecord]{
		{Size: 1}, {Size: 1, After: value.Set(token)}, {Size: MaxPageSize, Before: value.Set(token)},
	} {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []CursorRequest[cursorRecord]{
		{}, {Size: MaxPageSize + 1}, {Size: 1, After: value.Set(token), Before: value.Set(token)},
		{Size: 1, After: value.Set(Cursor[cursorRecord]{})},
	} {
		var input *CursorInputError
		if err := request.Validate(); !errors.As(err, &input) || !errors.Is(err, fault.Invalid) {
			t.Fatalf("request not classified as cursor input: %v", err)
		}
	}
	// Structural validation cannot prove the current query fingerprint. Execution
	// does, and a changed query is a typed input failure suitable for HTTP 400.
	changed := cursorQuery().Limit(2)
	_, err = changed.cursorPlan(CursorRequest[cursorRecord]{Size: 1, After: value.Set(token)})
	var input *CursorInputError
	if err == nil || errors.As(err, &input) {
		t.Fatal("invalid query declaration misclassified as user cursor")
	}
	_, err = readCursorBoundary(CursorRequest[cursorRecord]{Size: 1, After: value.Set(token)}, "different", first.fields, []bool{true})
	if !errors.As(err, &input) {
		t.Fatal("stale/foreign query token not classified as input", err)
	}
}

func TestCursorBoundaryDistinguishesInputFromMetadataFailures(t *testing.T) {
	first, err := cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	token, err := makeCursor(first.scope, first.fields, cursorRecord{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	request := CursorRequest[cursorRecord]{Size: 1, After: value.Set(token)}
	for _, required := range [][]bool{nil, {true, false}} {
		var input *CursorInputError
		_, err := readCursorBoundary(request, first.scope, first.fields, required)
		if err == nil || errors.As(err, &input) {
			t.Fatal("framework metadata failure became client input")
		}
	}
	fields := append([]RecordField[cursorRecord](nil), first.fields...)
	fields[0].decode = nil
	_, err = readCursorBoundary(request, first.scope, fields, []bool{true})
	var input *CursorInputError
	if err == nil || errors.As(err, &input) {
		t.Fatal("missing framework decoder became client input")
	}
	envelope, err := decodeCursor(token.Token())
	if err != nil {
		t.Fatal(err)
	}
	envelope.Values[0] = cursorValue{Kind: "string", Text: "private incompatible key"}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	request.After = value.Set(Cursor[cursorRecord]{token: base64.RawURLEncoding.EncodeToString(data)})
	_, err = readCursorBoundary(request, first.scope, first.fields, []bool{true})
	if !errors.As(err, &input) || !errors.Is(err, fault.Invalid) {
		t.Fatal("bad boundary value not classified as input")
	}
	if input.Error() != "invalid cursor input or query scope" {
		t.Fatal("input details in safe message")
	}
}

func TestCursorPageOutputValidationDoesNotClaimInputFailure(t *testing.T) {
	first, err := cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	token, err := makeCursor(first.scope, first.fields, cursorRecord{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []CursorPage[cursorRecord]{
		{Size: 2}, {Size: 2, Items: []cursorRecord{{ID: 7}}, Next: value.Set(token), Previous: value.Set(token)},
	} {
		if err := page.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, page := range []CursorPage[cursorRecord]{
		{}, {Size: 2, Next: value.Set(token)}, {Size: 1, Items: []cursorRecord{{ID: 1}, {ID: 2}}},
		{Size: 2, Items: []cursorRecord{{ID: 7}}, Next: value.Set(Cursor[cursorRecord]{})},
	} {
		var input *CursorInputError
		if err := page.Validate(); err == nil || errors.As(err, &input) {
			t.Fatal("invalid service result must remain a server failure", err)
		}
	}
}

func TestCursorCodecFailuresRemainServerErrors(t *testing.T) {
	first, err := cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	token, err := makeCursor(first.scope, first.fields, cursorRecord{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"error", "panic", "goexit", "invalid", "zero"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("private codec failure")
			c := codec.New[int64](func(v int64) (driver.Value, error) { return v, nil }, func(any) (int64, error) {
				switch mode {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "invalid":
					return 0, fault.New(fault.Invalid, "invalid key")
				}
				return 0, cause
			})
			if mode == "zero" {
				c = codec.Codec[int64]{}
			}
			field := NewRecordField("id", c, func(r cursorRecord) int64 { return r.ID })
			_, err := readCursorBoundary(CursorRequest[cursorRecord]{Size: 1, After: value.Set(token)}, first.scope, []RecordField[cursorRecord]{field}, []bool{true})
			var input *CursorInputError
			if err == nil || errors.As(err, &input) != (mode == "invalid") {
				t.Fatalf("codec %s classification: %v", mode, err)
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("codec cause lost")
			}
		})
	}
}
