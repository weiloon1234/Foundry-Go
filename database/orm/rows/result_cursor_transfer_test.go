package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestResultCursorCanonicalOrderAndBoundary(t *testing.T) {
	m := declareMembers()
	base := CursorFor(ModelRecord(m.Query().Limit(7).Offset(2)))
	id, rank := expr.At(base.Scope(), orm.KeyExpr(m.ID)), expr.At(base.Scope(), orm.NullableTextExpr(m.Nickname))
	q := base.OrderBy(rank.Desc()).UniqueBy(expr.Group(id))
	s, e := ormcore.CompileResultCursor(t.Context(), q.core)
	if e != nil || !strings.HasSuffix(s.SQLText, `ORDER BY "foundry_cursor"."nickname" DESC, "foundry_cursor"."id" ASC`) || !strings.Contains(s.SQLText, `LIMIT $1 OFFSET $2) AS "foundry_cursor"`) || !reflect.DeepEqual(s.Bindings, []any{int64(7), int64(2)}) {
		t.Fatal(s.SQLText, s.Bindings, e)
	}
	if len(base.core.Orders) != 0 || len(base.core.Unique) != 0 || len(q.core.Orders) != 1 {
		t.Fatal("canonicalization mutated builder")
	}
	plan, e := ormcore.PlanResultCursor(t.Context(), q.core, 2, "", false, false)
	if e != nil {
		t.Fatal(e)
	}
	item := member{ID: 42, Nickname: value.Of("seven")}
	token, e := plan.Token(unsafe.Pointer(&item))
	if e != nil {
		t.Fatal(e)
	}
	back, e := ormcore.PlanResultCursor(t.Context(), q.core, 2, token, true, true)
	if e != nil {
		t.Fatal(e)
	}
	// Remove the terminal lookahead to compare just the canonical boundary.
	back.Record.Source.Node.Limit = value.Optional[int]{}
	statement, e := ormcore.CompileRecord(t.Context(), back.Record)
	if e != nil || !strings.Contains(statement.SQLText, `"foundry_cursor"."nickname" > $3`) || !strings.HasSuffix(statement.SQLText, `"foundry_cursor"."id" DESC`) || !reflect.DeepEqual(statement.Bindings, []any{int64(7), int64(2), "seven", "seven", int64(42)}) {
		t.Fatal(statement.SQLText, statement.Bindings, e)
	}
	for _, bad := range []querycore.CursorValue{{Kind: "null"}, {Kind: "string", Text: "bad id"}} {
		env, e := querycore.DecodeCursor(token)
		if e != nil {
			t.Fatal(e)
		}
		env.Values[1] = bad
		data, e := json.Marshal(env)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := ormcore.PlanResultCursor(t.Context(), q.core, 1, base64.RawURLEncoding.EncodeToString(data), true, false); !errors.Is(e, fault.Invalid) {
			t.Fatal("required codec bypassed", e)
		}
	}
}
func TestResultCursorRejectsAmbiguousDeclarations(t *testing.T) {
	m := declareMembers()
	base := CursorFor(ModelRecord(m.Query()))
	id := expr.At(base.Scope(), orm.KeyExpr(m.ID))
	good := base.UniqueBy(expr.Group(id))
	for _, q := range []CursorQuery[member]{
		{}, CursorFor[member](nil), base, base.UniqueBy(expr.Group(id), expr.Group(id)), base.UniqueBy(expr.GroupKey[CursorScope[member]]{}),
		good.OrderBy(id.Asc(), id.Desc()), good.OrderBy(nil), good.OrderBy(expr.Count[CursorScope[member]]().Asc()), good.OrderBy(make([]expr.ProjectionOrder[CursorScope[member]], querycore.MaxCursorFields+1)...), base.UniqueBy(make([]expr.GroupKey[CursorScope[member]], querycore.MaxCursorFields+1)...),
	} {
		if _, e := ormcore.CompileResultCursor(t.Context(), q.core); !errors.Is(e, fault.Invalid) {
			t.Fatal("ambiguous cursor", e)
		}
	}
	for _, field := range []querycore.FieldRef{{Table: "foundry_cursor", Column: "missing"}, {Table: "foreign", Column: "id"}} {
		for _, identity := range []bool{false, true} {
			q := good
			if identity {
				q.core.Unique = []querycore.FieldRef{field}
			} else {
				q.core.Orders = []querycore.QueryOrder{{Field: field}}
			}
			if _, e := ormcore.CompileResultCursor(t.Context(), q.core); !errors.Is(e, fault.Invalid) {
				t.Fatal("unknown/foreign cursor field", e)
			}
		}
	}
	for _, fields := range []func() []ormcore.RecordField{nil, func() []ormcore.RecordField { return nil }, func() []ormcore.RecordField { return []ormcore.RecordField{{Name: "id"}} }} {
		q := good
		q.core.Record.Record.Fields = fields
		if _, e := ormcore.CompileResultCursor(t.Context(), q.core); !errors.Is(e, fault.Invalid) {
			t.Fatal("missing getters", e)
		}
	}
	if _, e := ormcore.CompileResultCursor(t.Context(), good.core); e != nil {
		t.Fatal("invalid branch mutated original", e)
	}
}
func TestNullableCursorTerminalBoundaryCompilesFalse(t *testing.T) {
	m := declareMembers()
	v := Values(DistinctScalar(SelectScalar(From(m.Query()), expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname))).Value())))
	c := ValueCursorFor(v)
	c = c.UniqueBy(expr.Group(c.Value()))
	plan, e := ormcore.PlanResultCursor(t.Context(), c.core, 2, "", false, false)
	if e != nil {
		t.Fatal(e)
	}
	item := value.Null[string]()
	token, e := plan.Token(unsafe.Pointer(&item))
	if e != nil {
		t.Fatal(e)
	}
	plan, e = ormcore.PlanResultCursor(t.Context(), c.core, 2, token, true, false)
	if e != nil {
		t.Fatal(e)
	}
	plan.Record.Source.Node.Limit = value.Optional[int]{}
	s, e := ormcore.CompileRecord(t.Context(), plan.Record)
	if e != nil || !strings.Contains(s.SQLText, "WHERE FALSE") || len(s.Bindings) != 0 {
		t.Fatal(s.SQLText, s.Bindings, e)
	}
}
func TestResultCursorCountsAppendedIdentityAgainstFieldBound(t *testing.T) {
	m := declareMembers()
	r := ModelRecord(m.Query())
	columns := []querycore.Column{{Name: "id"}}
	for i := range querycore.MaxCursorFields {
		columns = append(columns, querycore.Column{Name: fmt.Sprintf("sort_%d", i)})
	}
	r.core.Record.Columns = columns
	r.core.Source.Node.Source.Columns = columns
	r.core.Source.Node.Selections = querycore.ColumnSelections("members", columns)
	c := CursorFor(r)
	for _, col := range columns[1:] {
		c.core.Orders = append(c.core.Orders, querycore.QueryOrder{Field: querycore.FieldRef{Table: "foundry_cursor", Column: col.Name}})
	}
	c.core.Unique = []querycore.FieldRef{{Table: "foundry_cursor", Column: "id"}}
	if _, e := ormcore.CompileResultCursor(t.Context(), c.core); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "field bound") {
		t.Fatal("identity exceeded budget", e)
	}
}

func TestProjectionCursorGetterValidation(t *testing.T) {
	m := declareMembers()
	schema := declareRows()
	r := RecordOf(Select(From(m.Query()), schema, Map(schema.ID, expr.Value(orm.KeyExpr(m.ID))), Map(schema.Name, expr.Value(orm.TextExpr(m.Name)))))
	c := CursorFor(r).UniqueBy(expr.Group(At(CursorFor(r).Scope(), schema.ID)))
	fields := r.core.Record.Fields()
	if _, e := ormcore.CompileResultCursor(t.Context(), c.core); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"duplicate", "zero", "missing", "nil get", "nil decode"} {
		fs := append([]ormcore.RecordField(nil), fields...)
		switch kind {
		case "duplicate":
			fs = append(fs, fs[0])
		case "zero":
			fs[0] = ormcore.RecordField{}
		case "missing":
			fs[0].Name = "missing"
		case "nil get":
			fs[0].Get = nil
		case "nil decode":
			fs[0].Decode = nil
		}
		bad := c
		bad.core.Record.Record.Fields = func() []ormcore.RecordField { return fs }
		if _, e := ormcore.CompileResultCursor(t.Context(), bad.core); !errors.Is(e, fault.Invalid) {
			t.Fatal(kind, e)
		}
	}
	// Automatic typed getters replace the legacy separately declared getter codec.
	field := schema.ID
	fc := *field.core
	fc.column.Typ = reflect.TypeFor[string]()
	field.core = &fc
	bad := Select(From(m.Query()), schema, Map(field, expr.Value(orm.KeyExpr(m.ID))), Map(schema.Name, expr.Value(orm.TextExpr(m.Name))))
	if _, e := CompileRead(Read(bad)); !errors.Is(e, fault.Invalid) {
		t.Fatal("inconsistent getter type", e)
	}
}
