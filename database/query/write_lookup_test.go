package query

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type lookupDraftProbe struct{ calls int }

func (d *lookupDraftProbe) FoundryCreateMutation(Mutation[cursorRecord]) (Mutation[cursorRecord], error) {
	d.calls++
	return Mutation[cursorRecord]{}, nil
}

func TestLookupWriteValidationBeforeTransaction(t *testing.T) {
	q := cursorQuery()
	writer := &untouchedRelationWriter{}
	draft := &lookupDraftProbe{}
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	for _, bad := range []Query[cursorRecord]{For[cursorRecord]("records"), q.OrderBy(id.Asc()), q.Limit(1), q.Offset(1), q.WithRelationLimits(DefaultRelationLimits())} {
		if _, err := bad.FirstOrInsert(t.Context(), writer, draft); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid lookup query acquired transaction", err)
		}
	}
	for _, bad := range []CreateDraft[cursorRecord]{nil, (*lookupDraftProbe)(nil)} {
		if _, err := q.FirstOrInsert(t.Context(), writer, bad); !errors.Is(err, fault.Invalid) {
			t.Fatal("nil creation draft accepted", err)
		}
	}
	if _, err := q.PatchOrInsert(t.Context(), writer, draft, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil update callback accepted", err)
	}
	if _, err := q.FirstOrInsert(nil, writer, draft); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := q.FirstOrInsert(ctx, writer, draft); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if writer.calls != 0 || draft.calls != 0 {
		t.Fatal("invalid lookup prepared a draft or started a transaction")
	}
	if _, err := q.FirstOrInsert(t.Context(), writer, draft); err == nil || writer.calls != 1 || draft.calls != 0 {
		t.Fatal("lookup prepared a creation draft before selecting its branch", err)
	}
}

func TestLookupUpdateCallbackUsesSharedDepthLimit(t *testing.T) {
	ctx := t.Context()
	for range MaxLifecycleDepth {
		var err error
		ctx, err = writeHookContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	_, err := modelUpdateMutation(ctx, (*database.Tx)(nil), cursorRecord{}, func(context.Context, *database.Tx, cursorRecord) (Mutation[cursorRecord], error) {
		calls++
		return Mutation[cursorRecord]{}, nil
	})
	if !errors.Is(err, fault.Invalid) || calls != 0 {
		t.Fatal("lookup update escaped lifecycle depth limit", err)
	}
}
