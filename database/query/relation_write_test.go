package query

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func writableThrough() ThroughRelation[cursorRecord, cursorRecord, cursorRecord] {
	base := testThrough()
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	return ManyToMany(id, id, rank, id).Bind("Links", cursorQuery(), cursorQuery(), cursorQuery(), base.get, base.set)
}

func TestRelationWriteKeysPreserveStoredTypesAndNullability(t *testing.T) {
	r := writableThrough()
	keys, err := r.writeKeys(cursorRecord{ID: 4}, cursorRecord{ID: 8})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := ReadCreateDefaults(r.pivot, keys.defaults)
	if err != nil {
		t.Fatal(err)
	}
	local, err := MutationValue[cursorRecord, int64](defaults, "records", "id")
	if v, ok := local.Get(); err != nil || !ok || v != 4 {
		t.Fatal("source key lost its concrete stored value", err)
	}
	foreign, err := MutationValue[cursorRecord, value.Nullable[int64]](defaults, "records", "rank")
	v, present := foreign.Get()
	key, nonnull := v.Get()
	if err != nil || !present || !nonnull || key != 8 {
		t.Fatal("nullable pivot key became omitted or NULL", err)
	}
	statement, err := r.pivot.Where(keys.predicates...).Compile()
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{int64(4), int64(8)}) {
		t.Fatal("relation key predicates disagree with captured defaults", err)
	}
	r.spec.local = fieldRef{"records", "rank"}
	if _, err := r.writeKeys(cursorRecord{ID: 4}, cursorRecord{ID: 8}); !errors.Is(err, fault.Missing) {
		t.Fatal("NULL endpoint key was assigned", err)
	}
}

func TestRelationWriteBoundsAndUnsupportedReadOptions(t *testing.T) {
	r := writableThrough()
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	limited := r.WithWriteLimit(2)
	if bound, err := limited.validateWrite(); err != nil || bound != 2 {
		t.Fatal(bound, err)
	}
	if bound, err := r.validateWrite(); err != nil || bound != MaxRelationWriteRows || r.writeLimit.IsSet() {
		t.Fatal("limit mutated its reusable descriptor", err)
	}
	for _, bad := range []ThroughRelation[cursorRecord, cursorRecord, cursorRecord]{
		r.WithWriteLimit(0), r.WithWriteLimit(MaxRelationWriteRows + 1), r.OrderBy(id.Asc()), r.OrderByPivot(id.Asc()),
		r.With(testRelation(cursorQuery(), cursorQuery())), r.WithPivot(testRelation(cursorQuery(), cursorQuery())), testThrough(), {},
	} {
		if _, err := bad.validateWrite(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid relation write accepted", err)
		}
	}
	if _, err := r.Where(id.Eq(3)).WherePivot(id.Eq(4)).validateWrite(); err != nil {
		t.Fatal("valid scoped write rejected", err)
	}
}

func TestCreateDefaultsRejectForeignOrRepeatedAssignments(t *testing.T) {
	for _, defaults := range []Mutation[cursorRecord]{
		Change(Assign[cursorRecord]("other", "id", codec.Signed[int64](), int64(1))),
		Change(Assign[cursorRecord]("records", "missing", codec.Signed[int64](), int64(1))),
		Change(Assign[cursorRecord]("records", "id", codec.Signed[int64](), int64(1)), Assign[cursorRecord]("records", "id", codec.Signed[int64](), int64(2))),
	} {
		if _, err := ReadCreateDefaults(cursorQuery(), defaults); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid defaults escaped declaration validation", err)
		}
	}
}

type untouchedRelationWriter struct{ calls int }

func (w *untouchedRelationWriter) Transaction(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
	w.calls++
	return errors.New("unexpected transaction")
}

type nilRelationDraft struct{}

func (*nilRelationDraft) FoundryCreateMutation(Mutation[cursorRecord]) (Mutation[cursorRecord], error) {
	panic("nil draft invoked")
}

func TestInvalidRelationWriteFailsBeforeTransaction(t *testing.T) {
	w := &untouchedRelationWriter{}
	r := writableThrough()
	var draft *nilRelationDraft
	if _, err := r.Attach(t.Context(), w, cursorRecord{}, cursorRecord{}, draft); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := r.WithWriteLimit(0).Detach(t.Context(), w, cursorRecord{}, cursorRecord{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := r.ForceDetach(t.Context(), w, cursorRecord{}, cursorRecord{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if w.calls != 0 {
		t.Fatal("invalid relation acquired a transaction")
	}
}
