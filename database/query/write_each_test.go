package query

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPerModelWriteValidationBeforeTransaction(t *testing.T) {
	q := cursorQuery()
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	writer := &untouchedRelationWriter{}
	for _, limit := range []int{-1, 0, MaxPerModelWriteRows + 1} {
		if _, err := q.RemoveEach(t.Context(), writer, limit); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid limit accepted", err)
		}
	}
	for _, bad := range []Query[cursorRecord]{For[cursorRecord]("records"), q.OrderBy(id.Asc()), q.Limit(1), q.Offset(1), q.WithRelationLimits(DefaultRelationLimits())} {
		if _, err := bad.RemoveEach(t.Context(), writer, 3); !errors.Is(err, fault.Invalid) {
			t.Fatal("read options or missing metadata accepted", err)
		}
	}
	if _, err := q.PatchEach(t.Context(), writer, 3, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil draft callback accepted", err)
	}
	if _, err := q.RestoreEach(t.Context(), writer, 3); !errors.Is(err, fault.Invalid) {
		t.Fatal("ordinary model restored", err)
	}
	if _, err := q.ForceRemoveEach(t.Context(), writer, 3); !errors.Is(err, fault.Invalid) {
		t.Fatal("ordinary model force-deleted", err)
	}
	if _, err := q.Patch(t.Context(), writer, Mutation[cursorRecord]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("single-model write lost primary-key requirement", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := q.RemoveEach(ctx, writer, 3); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if writer.calls != 0 {
		t.Fatal("invalid input acquired a transaction")
	}
}

func TestPerModelCreationSharesBoundsAndDefersRequiredFields(t *testing.T) {
	q := cursorQuery()
	writer := &untouchedRelationWriter{}
	rows, err := q.InsertEach(t.Context(), writer, nil)
	if err != nil || rows == nil || len(rows) != 0 || writer.calls != 0 {
		t.Fatal("empty input acquired a transaction", err)
	}
	if _, err := q.InsertEach(t.Context(), writer, make([]Mutation[cursorRecord], MaxPerModelWriteRows+1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("row bound ignored", err)
	}
	foreign := Change(Assign[cursorRecord]("foreign", "id", codec.Signed[int64](), int64(1)))
	if _, err := q.InsertEach(t.Context(), writer, []Mutation[cursorRecord]{foreign}); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign assignment accepted", err)
	}
	if writer.calls != 0 {
		t.Fatal("invalid batch acquired a transaction")
	}
	// Before hooks may supply the absent fields; this probe stops at the
	// transaction boundary rather than pretending a missing field is final.
	if _, err := q.InsertEach(t.Context(), writer, []Mutation[cursorRecord]{{}}); err == nil || writer.calls != 1 {
		t.Fatal("required fields were evaluated before lifecycle", err)
	}
}

func TestPerModelWriteCompilerRetainsScopesAndPrimaryRules(t *testing.T) {
	q := cursorQuery()
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	scoped := q.Where(id.Gt(3))
	if _, err := scoped.modelWriteCompiler(updateModel, false); err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.mutationCompiler(updateModel); !errors.Is(err, fault.Invalid) {
		t.Fatal("single-model primary constraint lost", err)
	}
	predicate, err := modelWritePredicate(q, cursorRecord{ID: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.Where(predicate).mutationCompiler(updateModel); err != nil {
		t.Fatal("selected primary is not an ordinary write", err)
	}
	if _, err := lockedModelWriteCandidates[cursorRecord](t.Context(), (*database.Tx)(nil), q, deleteModel, 0); !errors.Is(err, fault.Invalid) {
		t.Fatal("selection validated the limit after I/O", err)
	}
}

func TestPerModelWriteCandidateIdentities(t *testing.T) {
	q := cursorQuery()
	primary, _ := q.definition.modelField("id")
	if err := validateModelWriteIdentities(t.Context(), primary, []cursorRecord{{ID: 1}, {ID: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := validateModelWriteIdentities(t.Context(), primary, []cursorRecord{{ID: 1}, {ID: 1}}); !errors.Is(err, database.TooManyRows) {
		t.Fatal("ambiguous identities reached callbacks", err)
	}
	nullable, _ := q.definition.modelField("rank")
	if err := validateModelWriteIdentities(t.Context(), nullable, []cursorRecord{{ID: 1}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("NULL primary identity accepted", err)
	}
}
