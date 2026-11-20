package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type planRecord struct {
	ID       int64
	Name     string
	Nickname value.Nullable[string]
}

type planRecordFields struct {
	Row[planRecord]
	ID       Field[planRecord, int64]
	Name     Field[planRecord, string]
	Nickname Field[planRecord, value.Nullable[string]]
}

func planBuild[S, R any](t *testing.T, build func(FoundryPlanBuilder[S, R], *R)) FoundryPlan[S, R] {
	t.Helper()
	plan, err := FoundryBuildPlan(sqlowner.Queries(), build)
	if err != nil {
		t.Fatalf("FoundryBuildPlan: %v", err)
	}
	return plan
}

func selectedOf[S, V any](e expr.RowValue[S, V]) expr.Selected[S, V] {
	return expr.AsSelected(expr.Row(e))
}

// TestPlanBehavesLikeSelect runs every consumer of a query and its record on a
// rows.Define + rows.Select query and on the equivalent plan query: compiled SQL
// and arguments, preflight, output sensitivity, RecordOf columns/fields/checks,
// field getters and decoders, execution, aliased records, set operations and
// cursor pagination including a decoded second page.
func TestPlanBehavesLikeSelect(t *testing.T) {
	m := declareMembers()
	sensitive := codec.String[string]().WithSensitiveValues()
	id, name, nick := selectedOf(expr.Value(m.ID)), selectedOf(expr.Value(m.Name)), selectedOf(expr.Value(m.Nickname))
	secret := expr.AsSelected(expr.Constant[member](sensitive, "secret-label"))
	slot := FoundryNewDynamicSlot[member, string](sqlowner.Queries())
	columns := []string{"id", "name", "nickname"}
	data := [][]driver.Value{{int64(1), "Ada", nil}, {int64(2), "Grace", "g"}, {int64(3), "Linus", nil}}
	sample := planRecord{ID: 42, Name: "Ada", Nickname: value.Null[string]()}

	define := func(nameField func(Builder[planRecord], *planRecord) Field[planRecord, string]) planRecordFields {
		return Define(func(b Builder[planRecord], r *planRecord) planRecordFields {
			return planRecordFields{Row: b.Row(), ID: NumberOf(b, &r.ID), Name: nameField(b, r), Nickname: NullableTextOf(b, &r.Nickname)}
		})
	}
	text := func(b Builder[planRecord], r *planRecord) Field[planRecord, string] { return TextOf(b, &r.Name) }
	secretText := func(b Builder[planRecord], r *planRecord) Field[planRecord, string] {
		return FieldOf(b, &r.Name, orm.Codec(sensitive))
	}
	selectWith := func(schema planRecordFields, nameValue expr.Selected[member, string]) Query[member, planRecord] {
		return Select(From(m.Query()), schema, MapSelected(schema.ID, id), MapSelected(schema.Name, nameValue), MapSelected(schema.Nickname, nick))
	}

	cases := []struct {
		name         string
		want         Query[member, planRecord]
		got          Query[member, planRecord]
		key          string
		sensitiveKey bool
	}{
		{
			name: "static model columns bound out of struct order",
			want: selectWith(define(text), name),
			got: planBuild(t, func(pb FoundryPlanBuilder[member, planRecord], r *planRecord) {
				FoundryBindPlanField(pb, &r.Nickname, nick)
				FoundryBindPlanField(pb, &r.Name, name)
				FoundryBindPlanField(pb, &r.ID, id)
			}).FoundryApply(From(m.Query()), sqlowner.Queries()),
			key: "id",
		},
		{
			name: "dynamic model column",
			want: selectWith(define(text), name),
			got: planBuild(t, func(pb FoundryPlanBuilder[member, planRecord], r *planRecord) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanDynamic(pb, &r.Name, slot)
				FoundryBindPlanField(pb, &r.Nickname, nick)
			}).FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicValue(slot, name, sqlowner.Queries())),
			key: "name",
		},
		{
			name: "sensitive scan codec",
			want: selectWith(define(secretText), name),
			got: planBuild(t, func(pb FoundryPlanBuilder[member, planRecord], r *planRecord) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanField(pb, &r.Name, name, FoundryScanWith(orm.Codec(sensitive), sqlowner.Queries()))
				FoundryBindPlanField(pb, &r.Nickname, nick)
			}).FoundryApply(From(m.Query()), sqlowner.Queries()),
			key: "name", sensitiveKey: true,
		},
		{
			name: "sensitive dynamic expression",
			want: selectWith(define(secretText), secret),
			got: planBuild(t, func(pb FoundryPlanBuilder[member, planRecord], r *planRecord) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanDynamic(pb, &r.Name, slot)
				FoundryBindPlanField(pb, &r.Nickname, nick)
			}).FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicValue(slot, secret, sqlowner.Queries())),
			key: "name", sensitiveKey: true,
		},
		{
			// The output stays sensitive, but the field codec decides record
			// field sensitivity, exactly as a rows.TextOf field mapped to it.
			name: "plain scan codec over a sensitive expression",
			want: selectWith(define(text), secret),
			got: planBuild(t, func(pb FoundryPlanBuilder[member, planRecord], r *planRecord) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanField(pb, &r.Name, secret, FoundryScanWith(orm.Codec(codec.String[string]()), sqlowner.Queries()))
				FoundryBindPlanField(pb, &r.Nickname, nick)
			}).FoundryApply(From(m.Query()), sqlowner.Queries()),
			key: "name", sensitiveKey: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.want.source.Err; err != nil {
				t.Fatalf("baseline: %v", err)
			}
			assertPlanLikeSelect(t, tc.want, tc.got, sample, columns, data, tc.key, tc.sensitiveKey)
		})
	}
}

func assertPlanLikeSelect[S, R any](t *testing.T, want, got Query[S, R], sample R, columns []string, data [][]driver.Value, key string, sensitiveKey bool) {
	t.Helper()
	ctx := t.Context()
	sameStatement := func(label string, w, g orm.Statement, werr, gerr error) {
		t.Helper()
		if werr != nil || gerr != nil {
			t.Fatalf("%s: compile errors want=%v got=%v", label, werr, gerr)
		}
		if w.SQL() != g.SQL() || !reflect.DeepEqual(w.Arguments(), g.Arguments()) {
			t.Fatalf("%s differs:\nwant %s %#v\ngot  %s %#v", label, w.SQL(), w.Arguments(), g.SQL(), g.Arguments())
		}
	}
	ws, werr := CompileRead(Read(want))
	gs, gerr := CompileRead(Read(got))
	sameStatement("CompileRead", ws, gs, werr, gerr)
	if werr, gerr := PreflightRead(ctx, Read(want)), PreflightRead(ctx, Read(got)); werr != nil || gerr != nil {
		t.Fatalf("PreflightRead want=%v got=%v", werr, gerr)
	}
	for i, w := range want.source.Node.Selections {
		g := got.source.Node.Selections[i]
		if w.Alias != g.Alias || w.Sensitive != g.Sensitive || w.NonCollatable != g.NonCollatable {
			t.Fatalf("selection %d: want %+v got %+v", i, w, g)
		}
	}
	wsens, werr := querycore.SelectSensitivity(want.source.Node)
	gsens, gerr := querycore.SelectSensitivity(got.source.Node)
	if werr != nil || gerr != nil || !reflect.DeepEqual(wsens, gsens) {
		t.Fatalf("output sensitivity want=%v,%v got=%v,%v", wsens, werr, gsens, gerr)
	}

	wr, gr := RecordOf(want), RecordOf(got)
	if wr.core.Source.Err != nil || gr.core.Source.Err != nil {
		t.Fatalf("RecordOf errors want=%v got=%v", wr.core.Source.Err, gr.core.Source.Err)
	}
	if !reflect.DeepEqual(wr.core.Record.Columns, gr.core.Record.Columns) || wr.core.Record.Type != gr.core.Record.Type {
		t.Fatalf("record columns want=%+v got=%+v", wr.core.Record.Columns, gr.core.Record.Columns)
	}
	if len(wr.core.Record.Checks) != len(gr.core.Record.Checks) {
		t.Fatalf("record checks want=%d got=%d", len(wr.core.Record.Checks), len(gr.core.Record.Checks))
	}
	wf, gf := wr.core.Record.Fields(), gr.core.Record.Fields()
	if len(wf) != len(gf) {
		t.Fatalf("record fields want=%d got=%d", len(wf), len(gf))
	}
	for i := range wf {
		if wf[i].Name != gf[i].Name || wf[i].Sensitive != gf[i].Sensitive {
			t.Fatalf("field %d want=%s/%v got=%s/%v", i, wf[i].Name, wf[i].Sensitive, gf[i].Name, gf[i].Sensitive)
		}
		wv, werr := wf[i].Get(unsafe.Pointer(&sample))
		gv, gerr := gf[i].Get(unsafe.Pointer(&sample))
		if !reflect.DeepEqual(wv, gv) || (werr == nil) != (gerr == nil) {
			t.Fatalf("field %s Get want=%#v,%v got=%#v,%v", wf[i].Name, wv, werr, gv, gerr)
		}
		for _, raw := range data {
			wv, werr := wf[i].Decode(raw[i])
			gv, gerr := gf[i].Decode(raw[i])
			if !reflect.DeepEqual(wv, gv) || (werr == nil) != (gerr == nil) {
				t.Fatalf("field %s Decode(%v) want=%#v,%v got=%#v,%v", wf[i].Name, raw[i], wv, werr, gv, gerr)
			}
		}
	}

	run := func(read func(*driverState) ([]R, error)) ([]R, *driverState) {
		t.Helper()
		state := &driverState{columns: columns, rows: data}
		items, err := read(state)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return items, state
	}
	wItems, wState := run(func(s *driverState) ([]R, error) { return Read(want).All(ctx, openTest(t, s)) })
	gItems, gState := run(func(s *driverState) ([]R, error) { return Read(got).All(ctx, openTest(t, s)) })
	if !reflect.DeepEqual(wItems, gItems) || wState.sql != gState.sql {
		t.Fatalf("Read.All want=%+v got=%+v", wItems, gItems)
	}

	wa, ga := AsRecord[left](wr, "report"), AsRecord[left](gr, "report")
	ws, werr = CompileRead(ReadRecord(SelectRecord(wa, Scope(wa))))
	gs, gerr = CompileRead(ReadRecord(SelectRecord(ga, Scope(ga))))
	sameStatement("AsRecord+SelectRecord", ws, gs, werr, gerr)
	wItems, _ = run(func(s *driverState) ([]R, error) {
		return ReadRecord(SelectRecord(wa, Scope(wa))).All(ctx, openTest(t, s))
	})
	gItems, _ = run(func(s *driverState) ([]R, error) {
		return ReadRecord(SelectRecord(ga, Scope(ga))).All(ctx, openTest(t, s))
	})
	if !reflect.DeepEqual(wItems, gItems) {
		t.Fatalf("ReadRecord(AsRecord) want=%+v got=%+v", wItems, gItems)
	}
	ws, werr = CompileRead(ReadSet(Union(wr, wr)))
	gs, gerr = CompileRead(ReadSet(Union(gr, gr)))
	sameStatement("Union", ws, gs, werr, gerr)

	wc, gc := planCursorKey(CursorFor(wr), key), planCursorKey(CursorFor(gr), key)
	_, werr = ormcore.CompileResultCursor(ctx, wc.core)
	_, gerr = ormcore.CompileResultCursor(ctx, gc.core)
	if sensitiveKey {
		for _, err := range []error{werr, gerr} {
			if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("sensitive cursor key was accepted: want=%v got=%v", werr, gerr)
			}
		}
		return
	}
	if werr != nil || gerr != nil {
		t.Fatalf("cursor compile want=%v got=%v", werr, gerr)
	}
	page := func(c CursorQuery[R], after value.Optional[orm.Cursor[R]]) (orm.CursorPage[R], *driverState) {
		t.Helper()
		state := &driverState{columns: columns, rows: data}
		p, err := c.Paginate(ctx, openTest(t, state), orm.CursorRequest[R]{Size: 1, After: after})
		if err != nil {
			t.Fatalf("Paginate: %v", err)
		}
		return p, state
	}
	wp, wState := page(wc, value.Optional[orm.Cursor[R]]{})
	gp, gState := page(gc, value.Optional[orm.Cursor[R]]{})
	wNext, wok := wp.Next.Get()
	gNext, gok := gp.Next.Get()
	if !reflect.DeepEqual(wp.Items, gp.Items) || !wok || !gok || wNext.Token() != gNext.Token() || wState.sql != gState.sql {
		t.Fatalf("cursor page want=%+v next=%v got=%+v next=%v", wp.Items, wNext.Token(), gp.Items, gNext.Token())
	}
	wp, wState = page(wc, value.Set(wNext))
	gp, gState = page(gc, value.Set(gNext))
	if !reflect.DeepEqual(wp.Items, gp.Items) || wState.sql != gState.sql || !reflect.DeepEqual(wState.arguments, gState.arguments) {
		t.Fatalf("decoded cursor page want=%+v %#v got=%+v %#v", wp.Items, wState.arguments, gp.Items, gState.arguments)
	}
}

// planCursorKey asserts uniqueness by one output column, as UniqueBy does for a
// scoped field; plans issue no rows.Field, so the key names the output column.
func planCursorKey[R any](c CursorQuery[R], column string) CursorQuery[R] {
	r := c.core.Record
	ref := ormcore.Reference{Table: r.Source.Node.Source.Name(), Owner: r.Record.Owner}
	c.core = ormcore.CursorUnique(c.core, []ormcore.Expression{{Node: querycore.FieldRef{Table: ref.Table, Column: column}, Refs: []ormcore.Reference{ref}}})
	return c
}

func TestPlanMatchesJoinedSelectSQL(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	a, b := As[left](m.Query(), "m"), As[right](o.Query(), "o")
	j := InnerJoin(a, b, On(expr.At(Scope(a), m.ID), expr.At(Scope(b), o.MemberID)))
	id := expr.At(RightScope(j, Scope(b)), o.ID)
	name := expr.At(LeftScope(j, Scope(a)), m.Name)
	schema := declareRows()
	want := OrderBy(Select(j, schema, Map(schema.Name, name), Map(schema.ID, id)).Where(expr.Eq(name, "Ada")), expr.Asc(id))
	plan := planBuild(t, func(pb FoundryPlanBuilder[Inner[Alias[left, member], Alias[right, order]], record], r *record) {
		FoundryBindPlanField(pb, &r.Name, selectedOf(name))
		FoundryBindPlanField(pb, &r.ID, selectedOf(id))
	})
	got := OrderBy(plan.FoundryApply(WhereSource(j, expr.Eq(name, "Ada")), sqlowner.Queries()), expr.Asc(id))
	ws, werr := CompileRead(Read(want))
	gs, gerr := CompileRead(Read(got))
	if werr != nil || gerr != nil || ws.SQL() != gs.SQL() || !reflect.DeepEqual(ws.Arguments(), gs.Arguments()) {
		t.Fatalf("joined SQL differs:\nwant %s %v\ngot  %s %v", ws.SQL(), werr, gs.SQL(), gerr)
	}
}

type planBadEnum string

func (planBadEnum) EnumDescriptor() enum.Descriptor[planBadEnum] {
	return enum.Descriptor[planBadEnum]{}
}

type planEnumModel struct {
	ID       int64
	Status   planBadEnum
	Optional value.Nullable[planBadEnum]
}

type planEnumTable struct {
	orm.Table[planEnumModel, int64]
	ID       orm.Primary[planEnumModel, int64]
	Status   orm.Enum[planEnumModel, planBadEnum]
	Optional orm.NullableEnum[planEnumModel, planBadEnum]
}

type planEnumRow struct {
	ID       int64
	Status   planBadEnum
	Optional value.Nullable[planBadEnum]
}

type planEnumFields struct {
	Row[planEnumRow]
	ID       Field[planEnumRow, int64]
	Status   Field[planEnumRow, planBadEnum]
	Optional Field[planEnumRow, value.Nullable[planBadEnum]]
}

// TestPlanEnumChecksLikeDefine shows an invalid enum descriptor fails the plan at
// build, preflight and record validation exactly where rows.Define reports it,
// and that a dynamic enum field retains its check for the request.
func TestPlanEnumChecksLikeDefine(t *testing.T) {
	tbl := orm.Define("plan_bad_enums", func(b orm.Builder[planEnumModel], r *planEnumModel) planEnumTable {
		t := orm.TableOf(b, &r.ID)
		return planEnumTable{Table: t, ID: orm.PrimaryOf(b, t.Key()), Status: orm.EnumOf(b, &r.Status), Optional: orm.NullableEnumOf(b, &r.Optional)}
	})
	schema := Define(func(b Builder[planEnumRow], r *planEnumRow) planEnumFields {
		return planEnumFields{Row: b.Row(), ID: NumberOf(b, &r.ID), Status: EnumOf(b, &r.Status), Optional: NullableEnumOf(b, &r.Optional)}
	})
	if err := Check(schema); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "invalid enum declaration") {
		t.Fatalf("rows.Check baseline = %v", err)
	}
	id, status, optional := selectedOf(expr.Value(tbl.ID)), selectedOf(expr.Value(tbl.Status)), selectedOf(expr.Value(tbl.Optional))
	for _, nullable := range []bool{false, true} {
		plan, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[planEnumModel, planEnumRow], r *planEnumRow) {
			FoundryBindPlanField(pb, &r.ID, id)
			if nullable {
				FoundryBindPlanField(pb, &r.Status, expr.AsSelected(expr.Constant[planEnumModel](codec.String[planBadEnum](), "x")))
				FoundryBindPlanField(pb, &r.Optional, optional)
			} else {
				FoundryBindPlanField(pb, &r.Status, status)
				FoundryBindPlanField(pb, &r.Optional, expr.AsSelected(expr.Constant[planEnumModel](codec.Nullable(codec.String[planBadEnum]()), value.Null[planBadEnum]())))
			}
		})
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "invalid enum declaration") {
			t.Fatalf("FoundryBuildPlan(nullable=%v) = %v, want enum declaration error", nullable, err)
		}
		q := plan.FoundryApply(From(tbl.Query()), sqlowner.Queries())
		for _, check := range []error{PreflightRead(t.Context(), Read(q)), RecordOf(q).core.Record.Checks[0]()} {
			if !errors.Is(check, fault.Invalid) || !strings.Contains(check.Error(), "invalid enum declaration") {
				t.Fatalf("plan check (nullable=%v) = %v", nullable, check)
			}
		}
	}

	slot := FoundryNewDynamicSlot[planEnumModel, planBadEnum](sqlowner.Queries())
	plan := planBuild(t, func(pb FoundryPlanBuilder[planEnumModel, planEnumRow], r *planEnumRow) {
		FoundryBindPlanField(pb, &r.ID, id)
		FoundryBindPlanDynamic(pb, &r.Status, slot)
		FoundryBindPlanField(pb, &r.Optional, expr.AsSelected(expr.Constant[planEnumModel](codec.Nullable(codec.String[planBadEnum]()), value.Null[planBadEnum]())))
	})
	q := plan.FoundryApply(From(tbl.Query()), sqlowner.Queries(), FoundryDynamicValue(slot, status, sqlowner.Queries()))
	record := RecordOf(q)
	if len(record.core.Record.Checks) != 1 {
		t.Fatalf("dynamic enum record checks = %d, want 1", len(record.core.Record.Checks))
	}
	for _, check := range []error{PreflightRead(t.Context(), Read(q)), record.core.Record.Checks[0]()} {
		if !errors.Is(check, fault.Invalid) || !strings.Contains(check.Error(), "invalid enum declaration") {
			t.Fatalf("dynamic enum check = %v", check)
		}
	}
}

// TestPlanScanWithKeepsColumnErrorAndCheck shows a FoundryScan replaces only the
// scan codec: the selected column's own error and enum check reach the plan
// exactly as on the column-codec path, for static and dynamic fields.
func TestPlanScanWithKeepsColumnErrorAndCheck(t *testing.T) {
	tbl := orm.Define("plan_bad_enums", func(b orm.Builder[planEnumModel], r *planEnumModel) planEnumTable {
		t := orm.TableOf(b, &r.ID)
		return planEnumTable{Table: t, ID: orm.PrimaryOf(b, t.Key()), Status: orm.EnumOf(b, &r.Status), Optional: orm.NullableEnumOf(b, &r.Optional)}
	})
	id, status := selectedOf(expr.Value(tbl.ID)), selectedOf(expr.Value(tbl.Status))
	optional := expr.AsSelected(expr.Constant[planEnumModel](codec.Nullable(codec.String[planBadEnum]()), value.Null[planBadEnum]()))
	scan := FoundryScanWith(orm.Codec(codec.String[planBadEnum]()), sqlowner.Queries())
	enumError := func(label string, err error) {
		t.Helper()
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "invalid enum declaration") {
			t.Fatalf("%s = %v, want the enum declaration error", label, err)
		}
	}
	for _, custom := range []bool{false, true} {
		var scans []FoundryScan[planBadEnum]
		if custom {
			scans = []FoundryScan[planBadEnum]{scan}
		}
		plan, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[planEnumModel, planEnumRow], r *planEnumRow) {
			FoundryBindPlanField(pb, &r.ID, id)
			FoundryBindPlanField(pb, &r.Status, status, scans...)
			FoundryBindPlanField(pb, &r.Optional, optional)
		})
		enumError(fmt.Sprintf("static build (custom=%v)", custom), err)
		q := plan.FoundryApply(From(tbl.Query()), sqlowner.Queries())
		enumError(fmt.Sprintf("static preflight (custom=%v)", custom), PreflightRead(t.Context(), Read(q)))
		enumError(fmt.Sprintf("static record (custom=%v)", custom), RecordOf(q).core.Record.Checks[0]())

		slot := FoundryNewDynamicSlot[planEnumModel, planBadEnum](sqlowner.Queries())
		plan = planBuild(t, func(pb FoundryPlanBuilder[planEnumModel, planEnumRow], r *planEnumRow) {
			FoundryBindPlanField(pb, &r.ID, id)
			FoundryBindPlanDynamic(pb, &r.Status, slot, scans...)
			FoundryBindPlanField(pb, &r.Optional, optional)
		})
		q = plan.FoundryApply(From(tbl.Query()), sqlowner.Queries(), FoundryDynamicValue(slot, status, sqlowner.Queries()))
		record := RecordOf(q)
		if len(record.core.Record.Checks) != 1 {
			t.Fatalf("dynamic record checks (custom=%v) = %d, want 1", custom, len(record.core.Record.Checks))
		}
		enumError(fmt.Sprintf("dynamic preflight (custom=%v)", custom), PreflightRead(t.Context(), Read(q)))
		enumError(fmt.Sprintf("dynamic record (custom=%v)", custom), record.core.Record.Checks[0]())
	}

	// A selected column's own declaration error survives a custom scan codec.
	m := declareMembers()
	memberID, name := selectedOf(expr.Value(m.ID)), selectedOf(expr.Value(m.Name))
	canary := errors.New("selected column canary")
	column := expr.FoundrySelectedColumn(name, sqlowner.Expressions())
	column.Err = canary
	broken := expr.FoundrySelectedValue[member](expr.FoundrySelected(name, sqlowner.Expressions()), column, sqlowner.Expressions())
	text := FoundryScanWith(orm.Codec(codec.String[string]()), sqlowner.Queries())
	for _, scans := range [][]FoundryScan[string]{nil, {text}} {
		_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) {
			FoundryBindPlanField(pb, &r.ID, memberID)
			FoundryBindPlanField(pb, &r.Name, broken, scans...)
		})
		if !errors.Is(err, canary) {
			t.Fatalf("static build (scans=%d) = %v, want the column error", len(scans), err)
		}
		slot := FoundryNewDynamicSlot[member, string](sqlowner.Queries())
		plan := planBuild(t, func(pb FoundryPlanBuilder[member, record], r *record) {
			FoundryBindPlanField(pb, &r.ID, memberID)
			FoundryBindPlanDynamic(pb, &r.Name, slot, scans...)
		})
		q := plan.FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicValue(slot, broken, sqlowner.Queries()))
		if _, err := CompileRead(Read(q)); !errors.Is(err, canary) {
			t.Fatalf("dynamic compile (scans=%d) = %v, want the column error", len(scans), err)
		}
	}
}

// TestPlanDeclarationErrorsLikeDefine covers the declaration errors rows.Define
// reports, and the plan's own slot and option errors.
func TestPlanDeclarationErrorsLikeDefine(t *testing.T) {
	m := declareMembers()
	id, name := selectedOf(expr.Value(m.ID)), selectedOf(expr.Value(m.Name))
	slot := FoundryNewDynamicSlot[member, string](sqlowner.Queries())
	type badTagRow struct {
		ID int64 `probe:"a" probe:"b"`
	}
	cases := map[string]func() error{
		"missing field": func() error {
			_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) { FoundryBindPlanField(pb, &r.ID, id) })
			return err
		},
		"repeated field": func() error {
			_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanField(pb, &r.Name, name)
				FoundryBindPlanField(pb, &r.Name, name)
			})
			return err
		},
		"foreign pointer": func() error {
			var other record
			_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanField(pb, &other.Name, name)
			})
			return err
		},
		"repeated scan option": func() error {
			_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanField(pb, &r.Name, name, FoundryScanWith(orm.Codec(codec.String[string]()), sqlowner.Queries()), FoundryScanWith(orm.Codec(codec.String[string]()), sqlowner.Queries()))
			})
			return err
		},
		"unprepared scan": func() error {
			_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanField(pb, &r.Name, name, FoundryScanWith(orm.Codec(codec.String[string]()), sqlowner.QuerySeal{}))
			})
			return err
		},
		"repeated slot": func() error {
			_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) {
				FoundryBindPlanField(pb, &r.ID, id)
				FoundryBindPlanDynamic(pb, &r.Name, slot)
				FoundryBindPlanDynamic(pb, &r.Name, slot)
			})
			return err
		},
		"malformed tag": func() error {
			_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, badTagRow], r *badTagRow) { FoundryBindPlanField(pb, &r.ID, id) })
			return err
		},
		"missing callback": func() error {
			_, err := FoundryBuildPlan[member, record](sqlowner.Queries(), nil)
			return err
		},
	}
	for label, run := range cases {
		if err := run(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s: err = %v, want fault.Invalid", label, err)
		}
	}
	type badTagFields struct {
		Row[badTagRow]
		ID Field[badTagRow, int64]
	}
	if err := Check(Define(func(b Builder[badTagRow], r *badTagRow) badTagFields {
		return badTagFields{b.Row(), NumberOf(b, &r.ID)}
	})); !errors.Is(err, fault.Invalid) {
		t.Fatalf("rows.Define accepted the malformed tag the plan rejects: %v", err)
	}

	plan := planBuild(t, func(pb FoundryPlanBuilder[member, record], r *record) {
		FoundryBindPlanField(pb, &r.ID, id)
		FoundryBindPlanDynamic(pb, &r.Name, slot)
	})
	other := FoundryNewDynamicSlot[member, string](sqlowner.Queries())
	dyn := FoundryDynamicValue(slot, name, sqlowner.Queries())
	for label, q := range map[string]Query[member, record]{
		"missing dynamic":  plan.FoundryApply(From(m.Query()), sqlowner.Queries()),
		"foreign slot":     plan.FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicValue(other, name, sqlowner.Queries())),
		"repeated dynamic": plan.FoundryApply(From(m.Query()), sqlowner.Queries(), dyn, dyn),
		"zero dynamic":     plan.FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicExpression[member]{}),
		"static extra": planBuild(t, func(pb FoundryPlanBuilder[member, record], r *record) {
			FoundryBindPlanField(pb, &r.ID, id)
			FoundryBindPlanField(pb, &r.Name, name)
		}).FoundryApply(From(m.Query()), sqlowner.Queries(), dyn),
	} {
		if _, err := CompileRead(Read(q)); !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s: CompileRead = %v, want fault.Invalid", label, err)
		}
	}
}

// TestPlanBridgeIsSealedOrTyped checks every exported plan entry point: each
// Foundry* bridge either requires the internal query capability, which
// applications cannot name, or is reachable only through a sealed builder, and
// rejects zero values at run time. Value types are fixed at compile time; the
// goway compile-fail cases rows_plan_dynamic_value and
// datatable_dynamic_expression_value prove a mismatch does not build.
func TestPlanBridgeIsSealedOrTyped(t *testing.T) {
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, "plan.go", mustRead(t, "plan.go"), 0)
	if err != nil {
		t.Fatal(err)
	}
	application := map[string]bool{"WhereSource": true, "GroupSource": true, "HavingSource": true, "DistinctSource": true}
	builderOnly := map[string]bool{"FoundryBindPlanField": true, "FoundryBindPlanDynamic": true}
	seen := map[string]bool{}
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			seen[d.Name.Name] = true
			signature := render(t, files, d.Type)
			switch {
			case application[d.Name.Name]:
				if strings.Contains(signature, "ormcore.") || strings.Contains(signature, "sqlowner.") {
					t.Fatalf("application helper %s exposes an internal type: %s", d.Name.Name, signature)
				}
			case !strings.HasPrefix(d.Name.Name, "Foundry"):
				t.Fatalf("exported plan entry point %s lacks the Foundry bridge prefix", d.Name.Name)
			case builderOnly[d.Name.Name]:
				if !strings.Contains(signature, "FoundryPlanBuilder[") {
					t.Fatalf("%s must require a sealed plan builder: %s", d.Name.Name, signature)
				}
			default:
				if !strings.Contains(signature, "seal sqlowner.QuerySeal") {
					t.Fatalf("%s must require the internal query capability: %s", d.Name.Name, signature)
				}
			}
			if strings.Contains(signature, "ormcore.") {
				t.Fatalf("%s exposes ormcore in its signature: %s", d.Name.Name, signature)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if s, ok := spec.(*ast.TypeSpec); ok && s.Name.IsExported() {
					seen[s.Name.Name] = true
					if !strings.HasPrefix(s.Name.Name, "Foundry") {
						t.Fatalf("exported plan type %s lacks the Foundry bridge prefix", s.Name.Name)
					}
				}
			}
		}
	}
	want := []string{"DistinctSource", "FoundryApply", "FoundryBindPlanDynamic", "FoundryBindPlanField", "FoundryBuildPlan", "FoundryCompileCount", "FoundryCompileRead", "FoundryDynamicExpression", "FoundryDynamicSlot", "FoundryDynamicValue", "FoundryNewDynamicSlot", "FoundryPlan", "FoundryPlanBuilder", "FoundryScan", "FoundryScanWith", "GroupSource", "HavingSource", "WhereSource"}
	got := make([]string, 0, len(seen))
	for name := range seen {
		got = append(got, name)
	}
	slices.Sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exported plan surface changed:\n got %v\nwant %v", got, want)
	}

	// Zero capabilities and zero values are rejected by every entry point.
	m := declareMembers()
	id, name := selectedOf(expr.Value(m.ID)), selectedOf(expr.Value(m.Name))
	var zeroSeal sqlowner.QuerySeal
	if _, err := FoundryBuildPlan(zeroSeal, func(FoundryPlanBuilder[member, record], *record) {}); !errors.Is(err, fault.Invalid) {
		t.Fatalf("FoundryBuildPlan(zero seal) = %v", err)
	}
	if FoundryNewDynamicSlot[member, string](zeroSeal).key != nil {
		t.Fatal("FoundryNewDynamicSlot(zero seal) issued a slot")
	}
	slot := FoundryNewDynamicSlot[member, string](sqlowner.Queries())
	plan := planBuild(t, func(pb FoundryPlanBuilder[member, record], r *record) {
		FoundryBindPlanField(pb, &r.ID, id)
		FoundryBindPlanDynamic(pb, &r.Name, slot)
	})
	rejected := map[string]Query[member, record]{
		"apply zero seal":  plan.FoundryApply(From(m.Query()), zeroSeal, FoundryDynamicValue(slot, name, sqlowner.Queries())),
		"value zero seal":  plan.FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicValue(slot, name, zeroSeal)),
		"value zero slot":  plan.FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicValue(FoundryDynamicSlot[member, string]{}, name, sqlowner.Queries())),
		"zero plan":        FoundryPlan[member, record]{}.FoundryApply(From(m.Query()), sqlowner.Queries()),
		"zero slot binder": planZeroSlot(t, m),
	}
	for label, q := range rejected {
		if _, err := CompileRead(Read(q)); !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s: CompileRead = %v, want fault.Invalid", label, err)
		}
	}
	for label, compile := range map[string]func(context.Context, Query[member, record], sqlowner.QuerySeal) (orm.Statement, error){
		"FoundryCompileRead": FoundryCompileRead[member, record], "FoundryCompileCount": FoundryCompileCount[member, record],
	} {
		q := plan.FoundryApply(From(m.Query()), sqlowner.Queries(), FoundryDynamicValue(slot, name, sqlowner.Queries()))
		if _, err := compile(t.Context(), q, zeroSeal); !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s(zero seal) = %v", label, err)
		}
		if _, err := compile(nil, q, sqlowner.Queries()); !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s(nil context) = %v", label, err)
		}
	}
	// A builder retained past FoundryBuildPlan, or a zero builder, binds nothing.
	var kept FoundryPlanBuilder[member, record]
	sealed := planBuild(t, func(pb FoundryPlanBuilder[member, record], r *record) {
		kept = pb
		FoundryBindPlanField(pb, &r.ID, id)
		FoundryBindPlanField(pb, &r.Name, name)
	})
	var r record
	FoundryBindPlanField(kept, &r.Name, name)
	FoundryBindPlanField(FoundryPlanBuilder[member, record]{}, &r.Name, name)
	if _, err := CompileRead(Read(sealed.FoundryApply(From(m.Query()), sqlowner.Queries()))); err != nil {
		t.Fatalf("sealed plan changed after build: %v", err)
	}
}

func planZeroSlot(t *testing.T, m members) Query[member, record] {
	_, err := FoundryBuildPlan(sqlowner.Queries(), func(pb FoundryPlanBuilder[member, record], r *record) {
		FoundryBindPlanField(pb, &r.ID, selectedOf(expr.Value(m.ID)))
		FoundryBindPlanDynamic(pb, &r.Name, FoundryDynamicSlot[member, string]{})
	})
	if !errors.Is(err, fault.Invalid) {
		t.Fatalf("zero slot binding = %v", err)
	}
	return Query[member, record]{source: ormcore.Source{Err: err}}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func render(t *testing.T, files *token.FileSet, node ast.Node) string {
	t.Helper()
	var b strings.Builder
	if err := printer.Fprint(&b, files, node); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
