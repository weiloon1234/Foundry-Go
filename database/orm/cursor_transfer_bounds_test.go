package orm

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"testing"
	"unsafe"
)

func FuzzCursorTransportPlanning(f *testing.F) {
	d := records()
	q := d.Query()
	p, err := newCursorPlan(q.state, 1, "", false, false)
	if err != nil {
		f.Fatal(err)
	}
	last := record{ID: 42}
	token, err := p.token(unsafe.Pointer(&last))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(token)
	f.Add("")
	f.Add("invalid")
	f.Fuzz(func(t *testing.T, token string) {
		cursor, err := ParseCursor[record](token)
		if err == nil {
			if cursor.Token() != token {
				t.Fatal("transport changed token")
			}
			_, _ = newCursorPlan(q.state, 1, token, true, false)
		}
	})
}
func TestCursorResourceBoundsIncludePrimaryTieBreaker(t *testing.T) {
	type item struct {
		ID     int64
		Sort0  int64
		Sort1  int64
		Sort2  int64
		Sort3  int64
		Sort4  int64
		Sort5  int64
		Sort6  int64
		Sort7  int64
		Sort8  int64
		Sort9  int64
		Sort10 int64
		Sort11 int64
		Sort12 int64
		Sort13 int64
		Sort14 int64
		Sort15 int64
	}
	type table struct {
		Table[item, int64]
		Sorts []Number[item, int64]
	}
	d := Define("bounded_cursor", func(b Builder[item], m *item) table {
		return table{TableOf(b, &m.ID), []Number[item, int64]{
			NumberOf(b, &m.Sort0), NumberOf(b, &m.Sort1), NumberOf(b, &m.Sort2), NumberOf(b, &m.Sort3), NumberOf(b, &m.Sort4), NumberOf(b, &m.Sort5), NumberOf(b, &m.Sort6), NumberOf(b, &m.Sort7), NumberOf(b, &m.Sort8), NumberOf(b, &m.Sort9), NumberOf(b, &m.Sort10), NumberOf(b, &m.Sort11), NumberOf(b, &m.Sort12), NumberOf(b, &m.Sort13), NumberOf(b, &m.Sort14), NumberOf(b, &m.Sort15)}}
	})
	if querycore.MaxCursorFields != len(d.Sorts) {
		t.Fatal("adapt test to shared bound")
	}
	orders := make([]Order[item], len(d.Sorts))
	for i, c := range d.Sorts {
		orders[i] = NumberAsc(c)
	}
	if _, err := newCursorPlan(d.Query().OrderBy(orders...).state, 1, "", false, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("appended key escaped cursor field budget", err)
	}
	if _, err := newCursorPlan(d.Query().OrderBy(orders[:len(orders)-1]...).state, 1, "", false, false); err != nil {
		t.Fatal("bounded order rejected", err)
	}
	if _, err := querycore.MakeCursor(strings.Repeat("a", 64), []driver.Value{strings.Repeat("x", querycore.MaxCursorBytes+1)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized token", err)
	}
}

func TestCursorInputValidationPreservesQueryOwnership(t *testing.T) {
	d := records()
	p, err := newCursorPlan(d.Query().state, 2, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	last := record{ID: 7}
	raw, err := p.token(unsafe.Pointer(&last))
	if err != nil {
		t.Fatal(err)
	}
	token, err := ParseCursor[record](raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []CursorRequest[record]{{Size: 1}, {Size: 1, After: value.Set(token)}, {Size: MaxPageSize, Before: value.Set(token)}} {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []CursorRequest[record]{{}, {Size: MaxPageSize + 1}, {Size: 1, After: value.Set(token), Before: value.Set(token)}, {Size: 1, After: value.Set(Cursor[record]{})}} {
		var input *CursorInputError
		if err := request.Validate(); !errors.As(err, &input) || !errors.Is(err, fault.Invalid) {
			t.Fatal("request classification", err)
		}
	}
	_, err = newCursorPlan(d.Query().Limit(2).state, 1, raw, true, false)
	var input *CursorInputError
	if err == nil || errors.As(err, &input) {
		t.Fatal("query metadata became client error", err)
	}
	_, err = newCursorPlan(d.Query().Where(TextEq(d.Name, "different")).state, 1, raw, true, false)
	if !errors.As(err, &input) {
		t.Fatal("foreign token is not client error", err)
	}
}

func TestSensitiveCursorKeysRejectBeforeExtraction(t *testing.T) {
	d := records()
	calls := 0
	d.core.primary.sensitive = true
	get := d.core.primary.get
	d.core.primary.get = func(p unsafe.Pointer) (driver.Value, error) { calls++; return get(p) }
	p, err := newCursorPlan(d.Query().state, 1, "", false, false)
	if !errors.Is(err, fault.Invalid) || len(p.fields) != 0 {
		t.Fatal("sensitive first page exported a usable token plan", err)
	}
	var statements []string
	if page, err := CursorPaginate(t.Context(), testDB(t, testConnector{statements: &statements}), d.Query(), CursorRequest[record]{Size: 1}); !errors.Is(err, fault.Invalid) || len(page.Items) != 0 || page.Next.IsSet() {
		t.Fatal("sensitive page output", page, err)
	}
	if calls != 0 || len(statements) != 0 {
		t.Fatal("sensitive key extracted before rejection", calls, statements)
	}
}
