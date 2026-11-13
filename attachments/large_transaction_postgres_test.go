package attachments

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresLargePreparedUnknownOuterCommitPreservesCandidates(t *testing.T) {
	for _, mode := range []int32{1, 2} {
		t.Run(map[int32]string{1: "committed", 2: "rolled-back"}[mode], func(t *testing.T) {
			var next atomic.Int32
			collection := Define(extensiontest.Members, "large-commit", largePolicy())
			f := openAttachmentsWithConnector(t, func(base driver.Connector) driver.Connector { return commitConnector{Connector: base, mode: &next} }, collection.Registration())
			owner := member(t, 1)
			old := attachmentOf(t, addText(t, f, collection, owner, "original"))
			candidate, err := collection.Prepare(t.Context(), f.manager, owner, Upload{Source: io.LimitReader(repeatedByte('x'), MaxBufferedBytes+1)})
			if err != nil {
				t.Fatal(err)
			}
			err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
				if _, err := collection.ReplaceIn(ctx, tx, f.manager, owner, candidate); err != nil {
					return err
				}
				next.Store(mode)
				return nil
			})
			if !errors.Is(err, database.CommitUnknown) {
				t.Fatal("outer commit was reported as known", err)
			}
			row := journal(t, f, candidate.Operation())
			key, err := storage.ParseKey(row.ObjectKey)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []storage.ObjectKey{key, old.key} {
				if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
					t.Fatal("unknown commit blindly removed a candidate", err)
				}
			}
			if err := f.manager.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(Dependencies{Store: f.Store, Disks: f.registry}, DefaultConfig(), collection.Registration())
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close(t.Context())
			if err := ageAttachmentIntents(t.Context(), f); err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.ReconcilePending(t.Context(), 10); err != nil {
				t.Fatal(err)
			}
			files, err := collection.List(t.Context(), restarted, owner)
			if err != nil || len(files) != 1 {
				t.Fatal("recovery lost ready membership", err)
			}
			if mode == 1 {
				if files[0].ID().String() != candidate.Operation().String() {
					t.Fatal("committed candidate disappeared")
				}
				assertObjectAbsent(t, f.disk, old.key)
			} else {
				if files[0].ID() != old.ID() {
					t.Fatal("rollback replaced original")
				}
				assertObjectAbsent(t, f.disk, key)
			}
		})
	}
}
