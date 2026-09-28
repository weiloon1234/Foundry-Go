package attachments

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type cyclicAttachmentError struct{ visits atomic.Int32 }

func (*cyclicAttachmentError) Error() string { panic("private attachment error formatted") }
func (e *cyclicAttachmentError) Unwrap() error {
	// A finite escape catches old unbounded searches without hanging the suite.
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

func TestTransactionOutcomeDoesNotTreatIncompleteGraphAsRollback(t *testing.T) {
	cycle := &cyclicAttachmentError{}
	if transactionOutcome(cycle) != database.Unknown {
		t.Fatal("incomplete graph claimed a known outcome")
	}
	if cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("transaction inspection was not bounded", cycle.visits.Load())
	}
}

func TestPostgresAttachmentCyclicCleanupFailureKeepsPublishedReplacementAndRetry(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	first := addText(t, f, testSingle, owner, "original")
	old := attachmentOf(t, first)
	cycle := &cyclicAttachmentError{}
	f.backend.setDelete(func(context.Context, storage.ObjectKey, storage.DeleteOptions) error {
		return storage.Failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, cycle)
	})
	result, err := testSingle.Replace(t.Context(), f.manager, owner, uploadText("replacement"))
	if err == nil || result.Publication != Published || !result.Attachment.IsSet() || len(result.PendingCleanup) != 1 || result.PendingCleanup[0] != first.Operation {
		t.Fatal("cleanup failure lost published replacement or pending cleanup")
	}
	latest := attachmentOf(t, result)
	assertBody(t, f, testSingle, owner, latest, "replacement")
	if row := journal(t, f, first.Operation); State(row.State) != CleanupPending || row.Attempts != 1 || row.LastFailure != "storage_delete_failed" {
		t.Fatal("cleanup failure was not journaled")
	}
	if _, err := f.disk.Stat(t.Context(), old.key, storage.ReadOptions{}); err != nil {
		t.Fatal("failed cleanup removed old bytes")
	}
	state, err := f.manager.Reconcile(t.Context(), first.Operation)
	if err == nil || state.State != CleanupPending || state.Attempts != 2 {
		t.Fatal("repeated cleanup failure lost retry state")
	}
	if cycle.visits.Load() == 0 || cycle.visits.Load() > 512 {
		t.Fatal("cleanup error inspection was not bounded", cycle.visits.Load())
	}
	if f.disk.Stats().Active != 0 {
		t.Fatal("cleanup failure retained disk")
	}
	f.backend.setDelete(nil)
	state, err = f.manager.Reconcile(t.Context(), first.Operation)
	if err != nil || state.State != Cleaned {
		t.Fatal("recovered cleanup did not complete", err)
	}
	assertBody(t, f, testSingle, owner, latest, "replacement")
	if err := f.manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.manager.Done():
	default:
		t.Fatal("cleanup failure retained manager ownership")
	}
}
