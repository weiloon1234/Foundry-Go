package attachments

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/extensions"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresAttachmentLifecycleJoinsOwnerTransaction(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	result := addText(t, f, testSingle, owner, "retain until commit")
	file := attachmentOf(t, result)
	err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=now() WHERE id=1`); err != nil {
			return err
		}
		return Cleanup(ctx, tx, f.manager, extensiontest.Members, owner, lifecycle.SoftDelete, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.disk.Stat(t.Context(), file.key, storage.ReadOptions{}); err != nil {
		t.Fatal("soft delete removed bytes", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=NULL WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("parent rollback")
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=1`); err != nil {
			return err
		}
		if err := Cleanup(ctx, tx, f.manager, extensiontest.Members, owner, lifecycle.ForceDelete, nil); err != nil {
			return err
		}
		if _, err := f.disk.Stat(ctx, file.key, storage.ReadOptions{}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	assertBody(t, f, testSingle, owner, file, "retain until commit")
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if err := Cleanup(ctx, tx, f.manager, extensiontest.Members, owner, lifecycle.Delete, nil); err == nil {
			t.Error("active owner cleanup accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=1`); err != nil {
			return err
		}
		return Cleanup(ctx, tx, f.manager, extensiontest.Members, owner, lifecycle.Delete, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if journal(t, f, result.Operation).State != string(Cleaned) {
		t.Fatal("committed deletion not cleaned")
	}
	if _, err := f.disk.Stat(t.Context(), file.key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
		t.Fatal("committed owner file retained", err)
	}
}

func TestPostgresAttachmentOrphansPreserveSoftDeletesAndUnsettledWriters(t *testing.T) {
	f := openAttachments(t)
	soft := addText(t, f, testSingle, member(t, 1), "soft")
	orphan := addText(t, f, testSingle, member(t, 2), "hard")
	f.backend.setPut(func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
		return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
	})
	uncertain, err := testSingle.Add(t.Context(), f.manager, member(t, 3), uploadText("unsettled"))
	if err == nil {
		t.Fatal("expected unknown put")
	}
	f.backend.setPut(nil)
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=now() WHERE id=1`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id IN (2,3)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cursor := Cursor{}
	found := map[OperationID]State{}
	scanned := 0
	for i := 0; i < 10; i++ {
		page, err := f.manager.InspectOrphans(t.Context(), extensiontest.Members.Name(), cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		scanned += page.Scanned
		for _, row := range page.Orphans {
			found[row.Operation] = row.State
		}
		if page.Next.IsZero() {
			break
		}
		if i == 0 {
			if _, err := f.manager.InspectOrphans(t.Context(), extensiontest.Others.Name(), page.Next, 1); err == nil {
				t.Fatal("foreign owner cursor accepted")
			}
		}
		cursor = page.Next
	}
	if scanned != 3 || len(found) != 2 || found[orphan.Operation] != Ready || found[uncertain.Operation] != Uncertain {
		t.Fatal("paged orphan report", scanned, found)
	}
	if _, ok := found[soft.Operation]; ok {
		t.Fatal("soft-deleted file considered orphan")
	}
	if _, err := f.manager.PruneOrphans(t.Context(), extensiontest.Members.Name(), []OperationID{orphan.Operation, uncertain.Operation}, nil); err == nil {
		t.Fatal("unsettled writer pruned")
	}
	if journal(t, f, orphan.Operation).State != string(Ready) {
		t.Fatal("failed prune partially retired earlier rows")
	}
	changed, err := f.manager.PruneOrphans(t.Context(), extensiontest.Members.Name(), []OperationID{orphan.Operation, soft.Operation}, nil)
	if err != nil || changed.Affected != 1 || changed.Publication != Published {
		t.Fatal("orphan prune", err)
	}
	state, err := f.manager.Settle(t.Context(), uncertain.Operation, WriterStoppedAndStorageSettled, nil)
	if err != nil || state.State != Cleaned {
		t.Fatal("settled missing upload", err)
	}
	page, err := f.manager.InspectOrphans(t.Context(), extensiontest.Members.Name(), Cursor{}, 100)
	if err != nil || len(page.Orphans) != 0 {
		t.Fatal("orphan maintenance left retired files", err)
	}
}

type inspectionClock struct{ now time.Time }

func (c inspectionClock) Now() time.Time { return c.now }
func TestPostgresAttachmentStorageInspectionPagesWithFixedCutoff(t *testing.T) {
	f := openAttachments(t)
	owned := addText(t, f, testSingle, member(t, 1), "owned")
	// Simulate a storage object with no journal. It is reported, never deleted.
	id, err := model.NewID[store.File]()
	if err != nil {
		t.Fatal(err)
	}
	key, err := objectKey(extensiontest.Members.Scope(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.disk.PutBytes(t.Context(), key, []byte("untracked"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	config := extensions.DefaultConfig()
	config.Schema = f.Schema
	config.Clock = inspectionClock{now: time.Now().UTC().Add(2 * time.Hour)}
	inspectionStore, err := extensions.New(f.DB, f.Store.Registry(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := inspectionStore.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	manager, err := New(Dependencies{Store: inspectionStore, Disks: f.registry}, DefaultConfig(), testSingle.Registration())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	options := StorageInspection{Disk: testDisk, Owner: extensiontest.Members.Name(), MinimumAge: time.Hour, Limit: 1}
	var candidates []storage.ObjectInfo
	scanned := 0
	for i := 0; i < 5; i++ {
		page, err := manager.InspectStorage(t.Context(), options)
		if err != nil {
			t.Fatal(err)
		}
		scanned += page.Scanned
		candidates = append(candidates, page.Candidates...)
		if page.Next.IsZero() {
			break
		}
		if i == 0 {
			bad := options
			bad.Cursor = page.Next
			bad.MinimumAge = time.Minute
			if _, err := manager.InspectStorage(t.Context(), bad); err == nil {
				t.Fatal("cursor policy changed")
			}
		}
		options.Cursor = page.Next
	}
	if scanned != 2 || len(candidates) != 1 || candidates[0].Key.String() != key.String() {
		t.Fatal("inspection failed to page past referenced objects")
	}
	if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
		t.Fatal("inspection mutated storage", err)
	}
	if journal(t, f, owned.Operation).State != string(Ready) {
		t.Fatal("inspection changed ownership")
	}
}
