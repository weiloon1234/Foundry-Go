package attachments

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"strings"
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

func TestPostgresPreparedAttachmentDoesNotTreatHostileDatabaseErrorAsMissing(t *testing.T) {
	var armed atomic.Pointer[errorgraphtest.Error]
	var calls atomic.Int32
	f := openAttachmentsWithConnector(t, func(base driver.Connector) driver.Connector {
		return extensiontest.InterceptQueries(base, func(statement string) error {
			if cause := armed.Load(); cause != nil && strings.Contains(statement, "extension_members") && strings.Contains(statement, "FOR UPDATE") {
				calls.Add(1)
				return cause
			}
			return nil
		})
	})
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		calls.Store(0)
		armed.Store(cause)
		prepared, err := testSingle.Prepare(t.Context(), f.manager, member(t, 1), uploadText("candidate"))
		armed.Store(nil)
		if err == nil || !errors.Is(err, cause) || prepared.IsZero() || calls.Load() != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 2048 {
			t.Fatal("attachment database failure became missing or escaped containment", calls.Load(), cause.Visits.Load())
		}
		// Prepare keeps a reconciliation identity even when staging fails. It
		// does not prove the upload exists or succeeded.
		if _, err := f.manager.Inspect(t.Context(), prepared.Operation()); !errors.Is(err, database.NotFound) {
			t.Fatal("failed owner lock recorded an upload")
		}
	}
}

func TestPostgresAttachmentPublicationContainsHostileDatabaseMatching(t *testing.T) {
	var armed atomic.Pointer[errorgraphtest.Error]
	var calls atomic.Int32
	f := openAttachmentsWithConnector(t, func(base driver.Connector) driver.Connector {
		return extensiontest.InterceptQueries(base, func(statement string) error {
			if cause := armed.Load(); cause != nil && strings.Contains(statement, "foundry_attachments") && strings.Contains(statement, "FOR UPDATE") {
				calls.Add(1)
				return cause
			}
			return nil
		})
	})
	owner := member(t, 1)
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		prepared, err := testSingle.Prepare(t.Context(), f.manager, owner, uploadText("candidate"))
		if err != nil {
			t.Fatal(err)
		}
		cause := &errorgraphtest.Error{Mode: mode}
		calls.Store(0)
		var result Result[extensiontest.Member, int64]
		err = f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
			armed.Store(cause)
			defer armed.Store(nil)
			var err error
			result, err = testSingle.AddIn(t.Context(), tx, f.manager, owner, prepared)
			return err
		})
		if err == nil || !errors.Is(err, cause) || result.Publication != Unpublished || result.Attachment.IsSet() || calls.Load() != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 2048 {
			t.Fatal("publication driver failure became invisibility or escaped containment")
		}
		if State(journal(t, f, prepared.Operation()).State) != Stored {
			t.Fatal("failed publication changed stored upload")
		}
	}
}

func TestPostgresAttachmentVariantContainsHostileDatabaseMatching(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &errorgraphtest.Error{Mode: mode}
			var armed atomic.Pointer[errorgraphtest.Error]
			var calls atomic.Int32
			policy := testPhotos.definition.policy
			policy.Variants = []Variant{testThumbnail}
			collection := Define(extensiontest.Members, "hostile_variants", policy, Hook[extensiontest.Member, int64]{AfterStored: func(context.Context, *database.Tx, Attachment[extensiontest.Member, int64]) error {
				armed.Store(cause)
				return nil
			}})
			f := openAttachmentsWithConnector(t, func(base driver.Connector) driver.Connector {
				return extensiontest.InterceptQueries(base, func(statement string) error {
					if cause := armed.Load(); cause != nil && strings.Contains(statement, "foundry_attachments") && strings.Contains(statement, "FOR UPDATE") {
						calls.Add(1)
						return cause
					}
					return nil
				})
			}, collection.Registration())
			result, err := collection.Add(t.Context(), f.manager, member(t, 1), pngUpload(t, 8, 8))
			armed.Store(nil)
			if err == nil || !errors.Is(err, cause) || result.Publication != Published || !result.Attachment.IsSet() || !result.PendingVariants || calls.Load() != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 2048 {
				t.Fatal("variant failure became superseded/success or escaped containment", calls.Load(), cause.Visits.Load())
			}
			if len(variantRows(t, f, result.Operation)) != 0 || State(journal(t, f, result.Operation).State) != Ready {
				t.Fatal("failed variant changed published original")
			}
		})
	}
}
