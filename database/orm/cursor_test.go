package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCursorRejectsForeignBoundariesBeforeSQL(t *testing.T) {
	r := records()
	q := r.Query().Where(r.Name.Ne("hidden")).OrderBy(r.Note.Asc(), r.ID.Desc())
	p, err := newCursorPlan(q.state, 2, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	last := record{ID: 5, Note: value.Null[string]()}
	token, err := p.token(unsafe.Pointer(&last))
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := query.ParseCursor[record](token)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		q       Query[record]
		request query.CursorRequest[record]
	}{
		{"scope", r.Query().Where(r.Name.Ne("different")).OrderBy(r.Note.Asc(), r.ID.Desc()), query.CursorRequest[record]{Size: 2, After: value.Set(cursor)}},
		{"order", r.Query().Where(r.Name.Ne("hidden")).OrderBy(r.Note.Desc(), r.ID.Desc()), query.CursorRequest[record]{Size: 2, After: value.Set(cursor)}},
		{"size_zero", q, query.CursorRequest[record]{}},
		{"size_large", q, query.CursorRequest[record]{Size: query.MaxPageSize + 1}},
		{"both", q, query.CursorRequest[record]{Size: 2, After: value.Set(cursor), Before: value.Set(cursor)}},
		{"zero_token", q, query.CursorRequest[record]{Size: 2, After: value.Set(query.Cursor[record]{})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var statements []string
			db := testDB(t, testConnector{statements: &statements})
			_, err := CursorPaginate(t.Context(), db, tc.q, tc.request)
			var input *query.CursorInputError
			if !errors.As(err, &input) || !errors.Is(err, fault.Invalid) || len(statements) != 0 {
				t.Fatal("invalid cursor not rejected before SQL", err)
			}
		})
	}
	// A structurally valid token with a bad key codec must remain an input error.
	bad, err := querycore.MakeCursor(p.scope, []driver.Value{nil, "not an integer"})
	if err != nil {
		t.Fatal(err)
	}
	badCursor, err := query.ParseCursor[record](bad)
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	_, err = CursorPaginate(t.Context(), db, q, query.CursorRequest[record]{Size: 2, After: value.Set(badCursor)})
	var input *query.CursorInputError
	if !errors.As(err, &input) || len(statements) != 0 {
		t.Fatal("bad codec boundary", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CursorPaginate(ctx, db, q, query.CursorRequest[record]{Size: 2}); !errors.Is(err, context.Canceled) || len(statements) != 0 {
		t.Fatal("canceled cursor sent SQL", err)
	}
	// Wrong model, even with identical declaration/SQL, retains a different scope.
	type another record
	var name Text[another, string]
	var note NullableText[another, string]
	other := Define("records", func(b Builder[another], v *another) Table[another, int64] {
		name = TextOf(b, &v.Name)
		note = NullableTextOf(b, &v.Note)
		NumberOf(b, &v.Amount)
		return TableOf(b, &v.ID)
	})
	foreign, err := query.ParseCursor[another](token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CursorPaginate(t.Context(), db, other.Query().Where(name.Ne("hidden")).OrderBy(note.Asc(), other.Key().Desc()), query.CursorRequest[another]{Size: 2, After: value.Set(foreign)})
	if !errors.As(err, &input) || len(statements) != 0 {
		t.Fatal("foreign model accepted", err)
	}
}

func TestCursorReadAndOutputFailurePublishNothing(t *testing.T) {
	r := records()
	for _, tc := range []struct {
		name string
		rows [][]driver.Value
	}{
		{"later_decode", [][]driver.Value{{int64(1), "ok", nil, "1"}, {"bad", "bad", nil, "1"}}},
		{"oversized_token", [][]driver.Value{{int64(1), strings.Repeat("x", query.MaxCursorBytes+1), nil, "1"}, {int64(2), "ok", nil, "1"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t, testConnector{columns: []string{"id", "display_name", "note", "amount"}, values: tc.rows})
			p, err := CursorPaginate(t.Context(), db, r.Query().OrderBy(r.Name.Asc()), query.CursorRequest[record]{Size: 1})
			if err == nil || p.Items != nil || p.Next.IsSet() {
				t.Fatal("partial cursor page published")
			}
		})
	}
}

func TestKeyChunksOwnBoundaryAndCloseBeforeCallbacks(t *testing.T) {
	r := records()
	closed, calls := 0, 0
	var bindings [][]any
	db := testDB(t, testConnector{bindings: &bindings, onClose: func() { closed++ }, respond: func(sql string) ([]string, [][]driver.Value) {
		calls++
		batch := [][]driver.Value{}
		if calls == 1 {
			batch = [][]driver.Value{{int64(5), "first", nil, "1"}}
		}
		if calls == 2 {
			batch = [][]driver.Value{{int64(3), "second", nil, "1"}}
		}
		return []string{"id", "display_name", "note", "amount"}, batch
	}})
	err := ChunkByKey(t.Context(), db, r.Query().OrderBy(r.ID.Desc()), r.ID, 1, func(items []record) error {
		if closed != calls {
			t.Fatal("callback ran with open rows")
		}
		items[0].ID = 999
		return nil
	})
	if err != nil || calls != 3 || bindings[1][0] != int64(5) || bindings[2][0] != int64(3) {
		t.Fatal("boundary changed with callback", err, bindings)
	}
	for _, q := range []Query[record]{r.Query().Offset(1), r.Query().OrderBy(r.Name.Asc())} {
		before := calls
		if err := ChunkByKey(t.Context(), db, q, r.ID, 1, func([]record) error { return nil }); !errors.Is(err, fault.Invalid) || calls != before {
			t.Fatal("invalid key ordering sent SQL", err)
		}
	}
	for _, size := range []int{0, MaxChunkSize + 1} {
		before := calls
		if err := EachByKey(t.Context(), db, r.Query(), r.ID, size, func(record) error { return nil }); !errors.Is(err, fault.Invalid) || calls != before {
			t.Fatal("invalid size sent SQL", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := EachByKey(ctx, db, r.Query(), r.ID, 1, func(record) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
