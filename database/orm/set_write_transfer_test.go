package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"reflect"
	"strings"
	"testing"
)

func TestSetBasedMutationsCompileWithoutPrimaryKeys(t *testing.T) {
	d := scopeTransferFields(WithGlobalScopes(func(d scopeTransferTable) []GlobalScope[scopeTransferRow] {
		return []GlobalScope[scopeTransferRow]{transferPositive(d)}
	}))
	q := WithScopeContext(d.Query().Where(NumberKeyGt(NumberKey(d.Key()), 10)), t.Context())
	assignment := NumberSet(d.Rank, 3)
	for _, test := range []struct {
		kind   querycore.MutationKind
		assign []Assignment[scopeTransferRow]
		sql    string
		args   []any
	}{
		{querycore.UpdateModel, []Assignment[scopeTransferRow]{assignment}, `UPDATE "scope_records" SET "rank" = $1 WHERE ("scope_records"."id" > $2) AND (("scope_records"."rank" > $3))`, []any{int64(3), int64(10), int64(0)}},
		{querycore.UpdateModel, []Assignment[scopeTransferRow]{NumberIncrement(d.Rank, 2)}, `SET "rank" = "rank" + $1 WHERE`, []any{int64(2), int64(10), int64(0)}},
		{querycore.UpdateModel, []Assignment[scopeTransferRow]{NumberDecrement(d.Rank, 2)}, `SET "rank" = "rank" - $1`, []any{int64(2), int64(10), int64(0)}},
		{querycore.DeleteModel, nil, `DELETE FROM "scope_records" WHERE ("scope_records"."id" > $1) AND (("scope_records"."rank" > $2))`, []any{int64(10), int64(0)}},
	} {
		p, e := planWrite(q.state, test.kind, test.assign, true)
		if e != nil {
			t.Fatal(e)
		}
		s, e := p.compile()
		if e != nil || !strings.Contains(s.SQLText, test.sql) || strings.Contains(s.SQLText, "RETURNING") || !reflect.DeepEqual(s.Bindings, test.args) {
			t.Fatal(s.SQLText, s.Bindings, e)
		}
	}
	keyAdjust := NumberIncrement(d.Rank, 1)
	keyAdjust.adjust = &querycore.Adjustment{Field: querycore.FieldRef{Table: "scope_records", Column: "id"}, Bind: keyAdjust.adjust.Bind}
	for _, as := range [][]Assignment[scopeTransferRow]{{assignment, NumberIncrement(d.Rank, 1)}, {keyAdjust}, nil} {
		p, e := planWrite(q.state, querycore.UpdateModel, as, true)
		if e == nil {
			_, e = p.compile()
		}
		if !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid set update", e)
		}
	}
	if _, e := planWrite(q.OrderBy(KeyAsc(d.Key())).state, querycore.DeleteModel, []Assignment[scopeTransferRow](nil), true); !errors.Is(e, fault.Invalid) {
		t.Fatal("ordered delete", e)
	}
	if _, e := planWrite(q.state, querycore.UpdateModel, []Assignment[scopeTransferRow]{assignment}, false); !errors.Is(e, fault.Invalid) {
		t.Fatal("single model lost identity", e)
	}
}
func TestSetBasedWritesRequireAcknowledgedHookSkipping(t *testing.T) {
	d := scopeTransferFields()
	writer := &noEachWriter{}
	if _, e := DeleteAll(t.Context(), writer, d.Query()); !errors.Is(e, fault.Invalid) || writer.calls != 0 {
		t.Fatal("unknown executor bypassed hooks", e, writer.calls)
	}
	if _, e := DeleteAll(t.Context(), writer, WithoutModelHooks(d.Query())); e == nil || errors.Is(e, fault.Invalid) || writer.calls != 1 {
		t.Fatal("acknowledgment did not enter transaction", e, writer.calls)
	}
	if _, e := Delete(t.Context(), writer, WithoutModelHooks(d.Query()), d.Key(), int64(1)); !errors.Is(e, fault.Invalid) || writer.calls != 1 {
		t.Fatal("per-model acknowledged skip", e, writer.calls)
	}
	if _, e := UpdateAll(t.Context(), writer, WithoutModelHooks(d.Query()), Assignment[scopeTransferRow]{}); !errors.Is(e, fault.Invalid) {
		t.Fatal("zero assignment", e)
	}
}
