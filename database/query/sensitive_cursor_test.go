package query

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestSensitiveCursorKeysRejectBeforeExtraction(t *testing.T) {
	type row struct{ Data string }
	calls := 0
	field := NewRecordField("data", codec.String[string]().WithSensitiveValues(), func(r row) string { calls++; return r.Data })
	request := CursorRequest[row]{Size: 1}
	if _, err := readCursorBoundary(request, "scope", []RecordField[row]{field}, []bool{true}); !errors.Is(err, fault.Invalid) {
		t.Fatal("first sensitive cursor page accepted", err)
	}
	if _, err := makeCursor("scope", []ModelField[row]{field}, row{Data: "private"}); !errors.Is(err, fault.Invalid) {
		t.Fatal("sensitive cursor exported", err)
	}
	if calls != 0 {
		t.Fatal("sensitive cursor extracted a value")
	}
}
