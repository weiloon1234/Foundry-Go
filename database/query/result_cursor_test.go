package query

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestResultCursorCanonicalOrderAndBoundary(t *testing.T) {
	base := CursorFor(cursorQuery().Limit(7).Offset(2))
	id := NewOrderedField[CursorScope[cursorRecord], int64](base.Scope().table, "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[CursorScope[cursorRecord], int64](base.Scope().table, "rank", codec.Signed[int64]())
	q := base.OrderBy(rank.Desc()).UniqueBy(id.Group())
	statement, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(statement.SQL(), `ORDER BY "foundry_cursor"."rank" DESC, "foundry_cursor"."id" ASC`) || !strings.Contains(statement.SQL(), `LIMIT $1 OFFSET $2) AS "foundry_cursor"`) || !reflect.DeepEqual(statement.Arguments(), []any{int64(7), int64(2)}) {
		t.Fatal("cursor lost source window or unique ordering", statement.SQL())
	}
	if len(base.orders) != 0 || len(base.unique) != 0 || len(q.orders) != 1 {
		t.Fatal("canonicalization mutated builder")
	}
	canonical, fields, required, err := q.canonical()
	if err != nil || !reflect.DeepEqual(required, []bool{false, true}) {
		t.Fatal("nullability lost", err)
	}
	scope, err := cursorStatementScope[cursorRecord](statement, "result-cursor", "id")
	if err != nil {
		t.Fatal(err)
	}
	token, err := makeCursor(scope, fields, cursorRecord{ID: 42, Rank: value.Of[int64](7)})
	if err != nil {
		t.Fatal(err)
	}
	navigation, err := readCursorBoundary(CursorRequest[cursorRecord]{Size: 2, Before: value.Set(token)}, scope, fields, required)
	if err != nil || !navigation.backward || !navigation.present || !reflect.DeepEqual(navigation.keys, []driver.Value{int64(7), int64(42)}) {
		t.Fatal("boundary lost typed values", err)
	}
	orders := cursorOrders(canonical.orders, navigation.backward)
	read := canonical.reader()
	read.node.orders = orderNodes(orders)
	read.node.predicates = []expression{cursorPredicate(orders, navigation.keys).expression}
	back, err := read.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(back.SQL(), `"foundry_cursor"."rank" > $3`) || !strings.HasSuffix(back.SQL(), `"foundry_cursor"."id" DESC`) || !reflect.DeepEqual(back.Arguments(), []any{int64(7), int64(2), int64(7), int64(7), int64(42)}) {
		t.Fatal("backward output boundary not parameterized after source", back.SQL(), back.Arguments())
	}
	for _, bad := range []cursorValue{{Kind: "null"}, {Kind: "string", Text: "bad id"}} {
		envelope, _ := decodeCursor(token.Token())
		envelope.Values[1] = bad
		data, _ := json.Marshal(envelope)
		forged := Cursor[cursorRecord]{token: base64.RawURLEncoding.EncodeToString(data)}
		if _, err := readCursorBoundary(CursorRequest[cursorRecord]{Size: 1, After: value.Set(forged)}, scope, fields, required); !errors.Is(err, fault.Invalid) {
			t.Fatal("required codec bypassed", err)
		}
	}
}

func TestResultCursorRejectsAmbiguousDeclarations(t *testing.T) {
	base := CursorFor(cursorQuery())
	id := NewOrderedField[CursorScope[cursorRecord], int64](base.Scope().table, "id", codec.Signed[int64]())
	unknown := NewOrderedField[CursorScope[cursorRecord], int64](base.Scope().table, "missing", codec.Signed[int64]())
	foreign := NewOrderedField[CursorScope[cursorRecord], int64]("foreign", "id", codec.Signed[int64]())
	good := base.UniqueBy(id.Group())
	for name, q := range map[string]CursorQuery[cursorRecord]{
		"zero": {}, "nil source": CursorFor[cursorRecord](nil), "identity missing": base,
		"identity repeated": base.UniqueBy(id.Group(), id.Group()), "identity unknown": base.UniqueBy(unknown.Group()),
		"identity foreign": base.UniqueBy(foreign.Group()), "identity zero": base.UniqueBy(Group[CursorScope[cursorRecord]]{}),
		"order repeated": good.OrderBy(id.Asc(), id.Desc()), "order unknown": good.OrderBy(unknown.Asc()), "order foreign": good.OrderBy(foreign.Asc()),
		"order nil": good.OrderBy(nil), "order computed": good.OrderBy(Count[CursorScope[cursorRecord]]().Asc()),
		"order bound":    good.OrderBy(make([]ProjectionOrder[CursorScope[cursorRecord]], MaxCursorFields+1)...),
		"identity bound": base.UniqueBy(make([]Group[CursorScope[cursorRecord]], MaxCursorFields+1)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid cursor declaration accepted", err)
			}
		})
	}
	missing := good
	metadata := *good.metadata
	metadata.fields = nil
	missing.metadata = &metadata
	if _, err := missing.Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing getters accepted", err)
	}
	if _, err := good.Compile(); err != nil {
		t.Fatal("invalid branch affected source", err)
	}
}

func TestProjectionGetterValidation(t *testing.T) {
	column := NewProjectionField[cursorRecord, int64]("id").Column()
	scan := func(database.Row) (cursorRecord, error) { return cursorRecord{}, nil }
	good := NewRecordField("id", codec.Signed[int64](), func(m cursorRecord) int64 { return m.ID })
	for _, fields := range [][]RecordField[cursorRecord]{nil, {good}} {
		if err := DefineProjection([]ProjectionColumn[cursorRecord]{column}, scan, fields...).Validate(); err != nil {
			t.Fatal("backward-compatible projection rejected", err)
		}
	}
	for _, fields := range [][]RecordField[cursorRecord]{
		{good, good}, {{}},
		{NewRecordField("missing", codec.Signed[int64](), func(m cursorRecord) int64 { return m.ID })},
		{NewRecordField("id", codec.String[string](), func(cursorRecord) string { return "wrong" })},
		{NewRecordField[cursorRecord, int64]("id", codec.Signed[int64](), nil)},
	} {
		if err := DefineProjection([]ProjectionColumn[cursorRecord]{column}, scan, fields...).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid projection getter accepted", err)
		}
	}
}

func TestNullableCursorTerminalBoundaryCompilesFalse(t *testing.T) {
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	values := SelectValue(cursorQuery(), rank.Value()).Distinct()
	c := ValueCursorFor(values)
	q, _, _, err := c.UniqueBy(c.Key()).canonical()
	if err != nil {
		t.Fatal(err)
	}
	q.node.predicates = []expression{cursorPredicate(q.orders, []driver.Value{nil}).expression}
	s, err := q.reader().Compile()
	if err != nil || !strings.Contains(s.SQL(), "WHERE FALSE") || len(s.Arguments()) != 0 {
		t.Fatal("all-NULL terminal boundary not empty", s.SQL(), err)
	}
}

func TestResultCursorCountsAppendedIdentityAgainstFieldBound(t *testing.T) {
	columns := []Column{{Name: "id"}}
	for i := range MaxCursorFields {
		columns = append(columns, Column{Name: fmt.Sprintf("sort_%d", i)})
	}
	source := ForModel(Define("records", "id", columns, func(database.Row) (cursorRecord, error) { return cursorRecord{}, nil }))
	cursor := CursorFor(source)
	for _, column := range columns[1:] {
		cursor = cursor.OrderBy(NewOrderedField[CursorScope[cursorRecord], int64](cursor.Scope().table, column.Name, codec.Signed[int64]()).Asc())
	}
	id := NewOrderedField[CursorScope[cursorRecord], int64](cursor.Scope().table, "id", codec.Signed[int64]())
	if _, err := cursor.UniqueBy(id.Group()).Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "field bound") {
		t.Fatal("appended identity exceeded ordering bound", err)
	}
}

func TestResultCursorFingerprintIncludesInputAndIdentity(t *testing.T) {
	input := cursorQuery()
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	fingerprint := func(source Query[cursorRecord], keys ...string) string {
		t.Helper()
		c := CursorFor(source)
		id := NewOrderedField[CursorScope[cursorRecord], int64](c.Scope().table, "id", codec.Signed[int64]())
		s, err := c.UniqueBy(id.Group()).Compile()
		if err != nil {
			t.Fatal(err)
		}
		hash, err := cursorStatementScope[cursorRecord](s, keys...)
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	base := fingerprint(input, "result-cursor", "id")
	for _, changed := range []Query[cursorRecord]{input.Where(rank.Gt(1)), input.OrderBy(rank.Asc()), input.Limit(1), input.Offset(1)} {
		if base == fingerprint(changed, "result-cursor", "id") {
			t.Fatal("changed input retained cursor fingerprint")
		}
	}
	if base == fingerprint(input, "result-cursor", "id", "rank") || base == fingerprint(input) {
		t.Fatal("identity/model namespace was not fingerprinted")
	}
}
