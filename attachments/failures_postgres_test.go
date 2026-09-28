package attachments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresAttachmentStorageFailureAndExplicitSettlement(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	original := attachmentOf(t, addText(t, f, testSingle, owner, "original"))
	var seen atomic.Int32
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		// The durable intent must be independently visible before provider I/O.
		err := f.Store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			var state string
			if err := database.ScanOne(ctx, tx, `SELECT state FROM foundry_attachments WHERE object_key=$1`, []any{key.String()}, &state); err != nil {
				return err
			}
			if state != string(Writing) {
				return fmt.Errorf("storage started without writing intent")
			}
			return nil
		})
		if err != nil {
			return storage.ObjectInfo{}, err
		}
		seen.Add(1)
		return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, nil)
	})
	result, err := testSingle.Replace(t.Context(), f.manager, owner, uploadText("rejected"))
	if err == nil || result.Publication != Unpublished || seen.Load() != 1 || journal(t, f, result.Operation).State != string(Cleaned) {
		t.Fatal("unchanged provider failure misclassified", err)
	}
	assertBody(t, f, testSingle, owner, original, "original")
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		object, err := f.backend.Backend.Put(ctx, key, source, options)
		if err != nil {
			return object, err
		}
		return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
	})
	result, err = testSingle.Replace(t.Context(), f.manager, owner, uploadText("acknowledgement lost"))
	if err == nil || result.Publication != Unpublished {
		t.Fatal("unknown storage publication accepted", err)
	}
	row := journal(t, f, result.Operation)
	if State(row.State) != Uncertain {
		t.Fatal("uncertain storage was reclaimed")
	}
	assertBody(t, f, testSingle, owner, original, "original")
	if state, err := f.manager.Reconcile(t.Context(), result.Operation); err == nil || state.State != Uncertain {
		t.Fatal("automatic reconciliation reclaimed unsettled write", err)
	}
	if _, err := f.manager.Settle(t.Context(), result.Operation, 0, nil); err == nil {
		t.Fatal("missing settlement assertion accepted")
	}
	f.backend.setPut(nil)
	settled, err := f.manager.Settle(t.Context(), result.Operation, WriterStoppedAndStorageSettled, nil)
	if err != nil || settled.State != Cleaned {
		t.Fatal("settled upload not cleaned", err)
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
		t.Fatal("settled orphan retained", err)
	}
	assertBody(t, f, testSingle, owner, original, "original")
}

func TestPostgresAttachmentFinalizationFailuresPreservePreviousFile(t *testing.T) {
	for _, mode := range []string{"veto", "panic", "goexit", "commit", "tamper", "owner-deleted"} {
		t.Run(mode, func(t *testing.T) {
			armed := false
			veto := errors.New("hook veto")
			collection := Define(extensiontest.Members, "guarded", testSingle.definition.policy, Hook[extensiontest.Member, int64]{AfterStored: func(ctx context.Context, tx *database.Tx, file Attachment[extensiontest.Member, int64]) error {
				if !armed {
					return nil
				}
				switch mode {
				case "veto":
					return veto
				case "panic":
					panic("hook panic")
				case "goexit":
					runtime.Goexit()
				case "commit":
					_, err := tx.Exec(ctx, `INSERT INTO deferred_attachment_check VALUES ('duplicate'),('duplicate')`)
					return err
				case "tamper":
					_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET digest=$1 WHERE id=$2`, fmt.Sprintf("%064d", 0), file.ID().String())
					return err
				case "owner-deleted":
					_, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=$1`, file.Owner().Key())
					return err
				}
				return nil
			}})
			f := openAttachments(t, collection.Registration())
			owner := member(t, 1)
			original := attachmentOf(t, addText(t, f, collection, owner, "original"))
			if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
				_, err := tx.Exec(ctx, `CREATE TABLE deferred_attachment_check (value text UNIQUE DEFERRABLE INITIALLY DEFERRED)`)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			armed = true
			result, err := collection.Replace(t.Context(), f.manager, owner, uploadText("rejected candidate"))
			if err == nil || result.Publication != Unpublished || result.Attachment.IsSet() {
				t.Fatal("failed finalization published", err)
			}
			if mode == "commit" && !errors.Is(err, database.UniqueViolation) {
				t.Fatal("expected real deferred commit rejection", err)
			}
			if row := journal(t, f, result.Operation); State(row.State) != Cleaned {
				t.Fatal("failed candidate not reconciled")
			}
			assertBody(t, f, collection, owner, original, "original")
		})
	}
}

func TestPostgresAttachmentCommittedHookAndCleanupFailureRemainPublished(t *testing.T) {
	armed := false
	afterCommit := errors.New("post-commit side effect")
	collection := Define(extensiontest.Members, "committed", testSingle.definition.policy, Hook[extensiontest.Member, int64]{AfterStored: func(_ context.Context, tx *database.Tx, _ Attachment[extensiontest.Member, int64]) error {
		if armed {
			return tx.AfterCommit(func(context.Context) error { return afterCommit })
		}
		return nil
	}})
	f := openAttachments(t, collection.Registration())
	owner := member(t, 1)
	first := addText(t, f, collection, owner, "old")
	old := attachmentOf(t, first)
	armed = true
	var deletes atomic.Int32
	f.backend.setDelete(func(ctx context.Context, key storage.ObjectKey, _ storage.DeleteOptions) error {
		err := f.Store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			var state string
			if err := database.ScanOne(ctx, tx, `SELECT state FROM foundry_attachments WHERE object_key=$1`, []any{key.String()}, &state); err != nil {
				return err
			}
			if state != string(CleanupPending) {
				return fmt.Errorf("delete ran before retirement commit")
			}
			return nil
		})
		if err != nil {
			return err
		}
		deletes.Add(1)
		return storage.Failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, nil)
	})
	result, err := collection.Replace(t.Context(), f.manager, owner, uploadText("new"))
	if !errors.Is(err, afterCommit) || result.Publication != Published || !result.Attachment.IsSet() || len(result.PendingCleanup) != 1 || result.PendingCleanup[0] != first.Operation || deletes.Load() != 1 {
		t.Fatal("committed failure hidden or reported rolled back", err)
	}
	latest := attachmentOf(t, result)
	assertBody(t, f, collection, owner, latest, "new")
	state, err := f.manager.Inspect(t.Context(), first.Operation)
	if err != nil || state.State != CleanupPending || state.Attempts != 1 {
		t.Fatal("retry intent missing", err)
	}
	f.backend.setDelete(nil)
	// A different object at the same key must not be deleted using stale pins.
	changed, err := f.disk.PutBytes(t.Context(), old.key, []byte("foreign replacement"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.manager.Reconcile(t.Context(), first.Operation)
	if !errors.Is(err, storage.PreconditionFailed) || state.State != CleanupPending {
		t.Fatal("changed object incorrectly cleaned", err)
	}
	body, _, err := f.disk.ReadBytes(t.Context(), old.key, 1024, storage.ReadOptions{})
	if err != nil || string(body) != "foreign replacement" {
		t.Fatal("conditional cleanup deleted foreign object", err)
	}
	if err := f.disk.Delete(t.Context(), old.key, storage.DeleteOptions{IfMatch: changed.Object.ETag}); err != nil {
		t.Fatal(err)
	}
	state, err = f.manager.Reconcile(t.Context(), first.Operation)
	if err != nil || state.State != Cleaned {
		t.Fatal("already absent cleanup not idempotent", err)
	}
	state, err = f.manager.Reconcile(t.Context(), first.Operation)
	if err != nil || state.State != Cleaned {
		t.Fatal("repeated cleanup", err)
	}
	assertBody(t, f, collection, owner, latest, "new")
}
