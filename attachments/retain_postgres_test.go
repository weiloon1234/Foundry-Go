package attachments

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresDetachedRetainedFilesSurviveOwnerAndMaintenance(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	upload := addText(t, f, testSingle, owner, "application now owns cleanup")
	file := attachmentOf(t, upload)
	result, err := testSingle.DetachKeepFile(t.Context(), f.manager, owner, file.ID())
	retained, ok := result.File.Get()
	if err != nil || !ok || result.Publication != Published || retained.Operation != upload.Operation {
		t.Fatal("detach keep file", err)
	}
	if files, err := testSingle.List(t.Context(), f.manager, owner); err != nil || len(files) != 0 {
		t.Fatal("retained file still belongs to collection", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=1`); err != nil {
			return err
		}
		return Cleanup(ctx, tx, f.manager, extensiontest.Members, owner, lifecycle.Delete, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if state, err := f.manager.Reconcile(t.Context(), upload.Operation); err != nil || state.State != Retained {
		t.Fatal("reconciler reclaimed deliberately retained file", err)
	}
	if result, err := f.manager.PruneOrphans(t.Context(), extensiontest.Members.Name(), []OperationID{upload.Operation}, nil); err != nil || result.Affected != 0 {
		t.Fatal("orphan pruning reclaimed retained file", err)
	}
	recovered, err := f.manager.RetainedObject(t.Context(), upload.Operation)
	if err != nil || recovered.Key.String() != retained.Key.String() || recovered.ETag != retained.ETag {
		t.Fatal("retained recovery lost pins", err)
	}
	body, _, err := f.disk.ReadBytes(t.Context(), retained.Key, 1024, storage.ReadOptions{IfMatch: retained.ETag, Version: retained.Version})
	if err != nil || string(body) != "application now owns cleanup" {
		t.Fatal("retained object disappeared", err)
	}
}
