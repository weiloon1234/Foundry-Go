package query

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type cursorRecord struct {
	ID   int64
	Rank value.Nullable[int64]
}

func cursorQuery() Query[cursorRecord] {
	return ForModel(Define("records", "id", []Column{{Name: "id"}, {Name: "rank", Nullable: true}}, func(database.Row) (cursorRecord, error) { return cursorRecord{}, nil },
		NewModelField("id", codec.Signed[int64](), func(m cursorRecord) int64 { return m.ID }),
		NewModelField("rank", codec.Nullable(codec.Signed[int64]()), func(m cursorRecord) value.Nullable[int64] { return m.Rank }),
	))
}

func TestPageRequestBounds(t *testing.T) {
	for _, request := range []PageRequest{{}, {0, 1}, {1, 0}, {1, -1}, {1, MaxPageSize + 1}, {math.MaxInt, 2}} {
		if _, err := request.offset(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid page accepted: %+v", request)
		}
	}
	if n, err := (PageRequest{math.MaxInt, 1}).offset(); err != nil || n != math.MaxInt-1 {
		t.Fatal("valid offset overflowed")
	}
	base := cursorQuery()
	q, err := base.paginationBase()
	if err != nil || len(q.orders) != 1 || q.orders[0].field.column != "id" || len(base.orders) != 0 {
		t.Fatal("pagination did not derive stable ordering")
	}
	for _, q := range []Query[cursorRecord]{base.Limit(0), base.Offset(1), base.OrderBy(q.orders[0], q.orders[0])} {
		if _, err := q.paginationBase(); !errors.Is(err, fault.Invalid) {
			t.Fatal("ambiguous pagination accepted")
		}
	}
}

func TestCursorBoundaryCompilationAndScope(t *testing.T) {
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	base := cursorQuery().OrderBy(rank.Asc())
	first, err := base.cursorPlan(CursorRequest[cursorRecord]{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := makeCursor(first.scope, first.fields, cursorRecord{ID: 42, Rank: value.Of[int64](7)})
	if err != nil {
		t.Fatal(err)
	}
	for _, backward := range []bool{false, true} {
		request := CursorRequest[cursorRecord]{Size: 3, After: value.Set(cursor)}
		if backward {
			request.After = value.Optional[Cursor[cursorRecord]]{}
			request.Before = value.Set(cursor)
		}
		plan, err := base.cursorPlan(request)
		if err != nil {
			t.Fatal(err)
		}
		statement, err := plan.query.Compile()
		if err != nil {
			t.Fatal(err)
		}
		comparison, direction, null := "rank\" > $1", " ASC", " IS NULL"
		if backward {
			comparison, direction, null = "rank\" < $1", " DESC", "rank\" IS NULL"
		}
		if !strings.Contains(statement.SQL(), comparison) || !strings.Contains(statement.SQL(), `"id"`+direction) {
			t.Fatalf("wrong cursor boundary: %s", statement.SQL())
		}
		if strings.Contains(statement.SQL(), null) == backward {
			t.Fatalf("wrong NULL boundary: %s", statement.SQL())
		}
		if !reflect.DeepEqual(statement.Arguments(), []any{int64(7), int64(7), int64(42), int64(4)}) {
			t.Fatalf("wrong bindings: %v", statement.Arguments())
		}
	}
	for name, q := range map[string]Query[cursorRecord]{"sort": cursorQuery().OrderBy(rank.Desc()), "scope": base.Where(rank.Gt(3)), "window": base.Limit(1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := q.cursorPlan(CursorRequest[cursorRecord]{Size: 2, After: value.Set(cursor)}); !errors.Is(err, fault.Invalid) {
				t.Fatal("cursor reused outside its query")
			}
		})
	}
	if _, err := base.cursorPlan(CursorRequest[cursorRecord]{Size: 2, After: value.Set(cursor), Before: value.Set(cursor)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("conflicting directions accepted")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, cursor), cursor.Token()) {
			t.Fatal("cursor formatting exposed its transport value")
		}
	}
	for _, bad := range []cursorValue{{Kind: "string", Text: "bad rank"}, {Kind: "int", Text: "9223372036854775808"}} {
		envelope, _ := decodeCursor(cursor.Token())
		envelope.Values[0] = bad
		data, _ := json.Marshal(envelope)
		badCursor := Cursor[cursorRecord]{token: base64.RawURLEncoding.EncodeToString(data)}
		if _, err := base.cursorPlan(CursorRequest[cursorRecord]{Size: 2, After: value.Set(badCursor)}); !errors.Is(err, fault.Invalid) {
			t.Fatal("cursor bypassed its typed codec")
		}
	}
}

func TestCursorTransportRoundTripsExactDriverValues(t *testing.T) {
	for _, original := range []driver.Value{nil, "", "private 值", []byte{0, 1, 255}, int64(math.MaxInt64), int64(math.MinInt64), 1.2345678901234567, true, false, time.Date(2026, 9, 11, 10, 11, 12, 123456000, time.UTC)} {
		encoded, err := encodeCursorValue(original)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := encoded.decode()
		if err != nil || !reflect.DeepEqual(original, decoded) {
			t.Fatalf("cursor value changed: %T", original)
		}
	}
	for _, token := range []string{"", "invalid", strings.Repeat("a", MaxCursorBytes+1), base64.RawURLEncoding.EncodeToString([]byte(`{"v":2}`)), base64.RawURLEncoding.EncodeToString([]byte(`{} {}`))} {
		if _, err := ParseCursor[cursorRecord](token); !errors.Is(err, fault.Invalid) {
			t.Fatal("malformed cursor accepted")
		}
	}
	for _, v := range []driver.Value{math.NaN(), math.Inf(1)} {
		if _, err := encodeCursorValue(v); err == nil {
			t.Fatal("nonfinite cursor accepted")
		}
	}
}

func FuzzCursorTransport(f *testing.F) {
	plan, err := cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 1})
	if err != nil {
		f.Fatal(err)
	}
	cursor, err := makeCursor(plan.scope, plan.fields, cursorRecord{ID: 42})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(cursor.Token())
	f.Add("")
	f.Add("invalid")
	f.Fuzz(func(t *testing.T, token string) {
		cursor, err := ParseCursor[cursorRecord](token)
		if err == nil {
			if cursor.Token() != token {
				t.Fatal("transport changed token")
			}
			_, _ = cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 1, After: value.Set(cursor)})
		}
	})
}

func TestCursorResourceBounds(t *testing.T) {
	columns := []Column{{Name: "id"}}
	var orders []Order[cursorRecord]
	for i := 0; i < MaxCursorFields; i++ {
		name := fmt.Sprintf("sort_%d", i)
		columns = append(columns, Column{Name: name})
		orders = append(orders, NewOrderedField[cursorRecord, int64]("records", name, codec.Signed[int64]()).Asc())
	}
	q := ForModel(Define("records", "id", columns, func(database.Row) (cursorRecord, error) { return cursorRecord{}, nil })).OrderBy(orders...)
	if _, err := q.cursorPlan(CursorRequest[cursorRecord]{Size: 1}); !errors.Is(err, fault.Invalid) {
		t.Fatal("cursor field bound ignored appended primary key")
	}
	fields := []ModelField[cursorRecord]{NewModelField("large", codec.String[string](), func(cursorRecord) string { return strings.Repeat("x", MaxCursorBytes+1) })}
	if _, err := makeCursor(strings.Repeat("a", 64), fields, cursorRecord{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized output cursor accepted")
	}
}

func BenchmarkCursorPlan(b *testing.B) {
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	q := cursorQuery().OrderBy(rank.Desc())
	plan, err := q.cursorPlan(CursorRequest[cursorRecord]{Size: 20})
	if err != nil {
		b.Fatal(err)
	}
	token, err := makeCursor(plan.scope, plan.fields, cursorRecord{ID: 42, Rank: value.Of[int64](7)})
	if err != nil {
		b.Fatal(err)
	}
	request := CursorRequest[cursorRecord]{Size: 20, After: value.Set(token)}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		plan, err := q.cursorPlan(request)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := plan.query.Compile(); err != nil {
			b.Fatal(err)
		}
	}
}
