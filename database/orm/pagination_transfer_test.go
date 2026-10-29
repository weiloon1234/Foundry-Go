package orm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPageRequestBounds(t *testing.T) {
	for _, r := range []PageRequest{{}, {Number: 0, Size: 1}, {Number: 1, Size: 0}, {Number: 1, Size: -1}, {Number: 1, Size: MaxPageSize + 1}, {Number: math.MaxInt, Size: 2}} {
		if _, err := querycore.PageOffset(r); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid page accepted", r, err)
		}
	}
	if n, err := querycore.PageOffset(PageRequest{Number: math.MaxInt, Size: 1}); err != nil || n != math.MaxInt-1 {
		t.Fatal(n, err)
	}
	d := records()
	base := d.Query()
	_, paged, err := pageStates(base.state, Page(1, 2))
	if err != nil || len(paged.ast.Orders) != 1 || paged.ast.Orders[0].Field.Column != "id" || len(base.state.ast.Orders) != 0 {
		t.Fatal("stable ordering mutated base", err)
	}
	for _, q := range []Query[record]{base.Limit(0), base.Offset(1), base.OrderBy(KeyAsc(d.ID), KeyAsc(d.ID))} {
		if _, _, err := pageStates(q.state, Page(1, 2)); !errors.Is(err, fault.Invalid) {
			t.Fatal("ambiguous pagination", err)
		}
	}
}

type cursorTransferRecord struct {
	ID   int64
	Rank value.Nullable[int64]
}
type cursorTransferFields struct {
	Table[cursorTransferRecord, int64]
	Rank NullableNumber[cursorTransferRecord, int64]
}

func cursorTransferTable() cursorTransferFields {
	return Define("records", func(b Builder[cursorTransferRecord], m *cursorTransferRecord) cursorTransferFields {
		return cursorTransferFields{TableOf(b, &m.ID), NullableNumberOf(b, &m.Rank)}
	})
}
func TestCursorBoundaryCompilationAndScope(t *testing.T) {
	d := cursorTransferTable()
	base := d.Query().OrderBy(NullableNumberAsc(d.Rank))
	first, err := newCursorPlan(base.state, 2, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	item := cursorTransferRecord{ID: 42, Rank: value.Of[int64](7)}
	token, err := first.token(unsafe.Pointer(&item))
	if err != nil {
		t.Fatal(err)
	}
	for _, back := range []bool{false, true} {
		plan, err := newCursorPlan(base.state, 3, token, true, back)
		if err != nil {
			t.Fatal(err)
		}
		s, err := compileRead(plan.query, querycore.ReadModels)
		if err != nil {
			t.Fatal(err)
		}
		comparison, direction, null := `rank" > $1`, " ASC", " IS NULL"
		if back {
			comparison, direction, null = `rank" < $1`, " DESC", `rank" IS NULL`
		}
		if !strings.Contains(s.SQLText, comparison) || !strings.Contains(s.SQLText, `"id"`+direction) || strings.Contains(s.SQLText, null) == back || !reflect.DeepEqual(s.Bindings, []any{int64(7), int64(7), int64(42), int64(4)}) {
			t.Fatal(s.SQLText, s.Bindings)
		}
	}
	for _, q := range []Query[cursorTransferRecord]{d.Query().OrderBy(NullableNumberDesc(d.Rank)), base.Where(NullableNumberGt(d.Rank, 3)), base.Limit(1)} {
		if _, err := newCursorPlan(q.state, 2, token, true, false); !errors.Is(err, fault.Invalid) {
			t.Fatal("foreign token scope accepted", err)
		}
	}
	cursor, err := ParseCursor[cursorTransferRecord](token)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, cursor), token) {
			t.Fatal("token disclosed")
		}
	}
	if err := (CursorRequest[cursorTransferRecord]{Size: 2, After: value.Set(cursor), Before: value.Set(cursor)}).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("conflicting directions accepted", err)
	}
	for _, bad := range []querycore.CursorValue{{Kind: "string", Text: "bad rank"}, {Kind: "int", Text: "9223372036854775808"}} {
		envelope, err := querycore.DecodeCursor(token)
		if err != nil {
			t.Fatal(err)
		}
		envelope.Values[0] = bad
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		text := base64.RawURLEncoding.EncodeToString(data)
		if _, err := newCursorPlan(base.state, 2, text, true, false); !errors.Is(err, fault.Invalid) {
			t.Fatal("typed cursor codec bypassed", err)
		}
	}
}
