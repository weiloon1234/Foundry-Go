package attachments

import (
	"context"
	"crypto/sha256"
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
			wantID, wantSize := old.ID().String(), int64(len("original"))
			wantDigest := sha256.Sum256([]byte("original"))
			if mode == 1 {
				wantID, wantSize = candidate.Operation().String(), MaxBufferedBytes+1
				hash := sha256.New()
				if n, err := io.Copy(hash, io.LimitReader(repeatedByte('x'), wantSize)); err != nil || n != wantSize {
					t.Fatal("expected survivor digest", n, err)
				}
				copy(wantDigest[:], hash.Sum(nil))
			}
			assertSurvivor := func(manager *Manager) {
				t.Helper()
				files, err := collection.List(t.Context(), manager, owner)
				if err != nil || len(files) != 1 || files[0].ID().String() != wantID {
					t.Fatal("survivor membership changed", err)
				}
				body, file, err := collection.Open(t.Context(), manager, owner, files[0].ID())
				if err != nil {
					t.Fatal("survivor could not be opened", err)
				}
				hash := sha256.New()
				n, readErr := io.Copy(hash, body)
				closeErr := body.Close()
				var got [sha256.Size]byte
				copy(got[:], hash.Sum(nil))
				if readErr != nil || closeErr != nil || n != wantSize || file.Info().Size != wantSize || got != wantDigest {
					t.Fatal("survivor bytes changed", n, readErr, closeErr)
				}
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
			assertSurvivor(f.manager)
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
			assertSurvivor(restarted)
			if mode == 1 {
				assertObjectAbsent(t, f.disk, old.key)
			} else {
				assertObjectAbsent(t, f.disk, key)
			}
		})
	}
}
