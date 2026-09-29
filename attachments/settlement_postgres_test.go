package attachments

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresAttachmentPendingSweepSettlesOnlyAgedUnambiguousWrites(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	lost := func(publish func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) error) Result[extensiontest.Member, int64] {
		t.Helper()
		f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
			if err := publish(ctx, key, source, options); err != nil {
				return storage.ObjectInfo{}, err
			}
			return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
		})
		defer f.backend.setPut(nil)
		result, err := testMultiple.Add(t.Context(), f.manager, owner, uploadText("intended bytes"))
		if err == nil || journal(t, f, result.Operation).State != string(Uncertain) {
			t.Fatal("lost acknowledgement was not uncertain", err)
		}
		return result
	}
	published := lost(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) error {
		_, err := f.backend.Backend.Put(ctx, key, source, options)
		return err
	})
	absent := lost(func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) error { return nil })
	foreign := lost(func(ctx context.Context, key storage.ObjectKey, _ io.Reader, _ storage.PutOptions) error {
		_, err := f.backend.Backend.Put(ctx, key, strings.NewReader("other bytes of another size"), storage.PutOptions{})
		return err
	})
	// Recent intents may still have a running writer or provider request.
	if _, err := f.manager.ReconcilePending(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	for _, result := range []Result[extensiontest.Member, int64]{published, absent, foreign} {
		if state := journal(t, f, result.Operation).State; state != string(Uncertain) {
			t.Fatal("recent unresolved write was settled by age", state)
		}
	}
	err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET updated_at = updated_at - interval '2 hours' WHERE state = $1`, string(Uncertain))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := f.manager.ReconcilePending(t.Context(), 10)
	if err != nil || len(results) != 3 {
		t.Fatal("aged unresolved writes were not inspected", err, len(results))
	}
	for _, result := range []Result[extensiontest.Member, int64]{published, absent} {
		row := journal(t, f, result.Operation)
		if row.State != string(Cleaned) {
			t.Fatal("unambiguous write was not settled", row.State, row.LastFailure)
		}
		key, err := storage.ParseKey(row.ObjectKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
			t.Fatal("settled orphan retained", err)
		}
	}
	row := journal(t, f, foreign.Operation)
	if row.State != string(Uncertain) || row.LastFailure != "settlement_ambiguous" {
		t.Fatal("ambiguous storage was settled automatically", row.State, row.LastFailure)
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	if body, _, err := f.disk.ReadBytes(t.Context(), key, 1024, storage.ReadOptions{}); err != nil || string(body) != "other bytes of another size" {
		t.Fatal("ambiguous object was deleted", err)
	}
}
