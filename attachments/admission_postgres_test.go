package attachments

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestPostgresAttachmentReadsWritesAndOwnerCleanupUseSeparateAdmission(t *testing.T) {
	f := openAttachments(t)
	config := DefaultConfig()
	config.MaxActive, config.MaxReads = 1, 2
	m, err := New(Dependencies{Store: f.Store, Disks: f.registry}, config, testSingle.Registration(), testMultiple.Registration())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	reader, deleted := member(t, 1), member(t, 2)
	if _, err := testMultiple.Add(t.Context(), m, reader, uploadText("readable")); err != nil {
		t.Fatal(err)
	}
	if _, err := testSingle.Add(t.Context(), m, deleted, uploadText("owner cleanup")); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		once.Do(func() { close(entered) })
		<-release
		return f.backend.Backend.Put(ctx, key, source, options)
	})
	slow := make(chan error, 1)
	go func() {
		_, err := testMultiple.Add(context.Background(), m, reader, uploadText("slow write"))
		slow <- err
	}()
	<-entered
	defer func() {
		close(release)
		if err := <-slow; err != nil {
			t.Error(err)
		}
	}()
	// The only write slot is busy; reads keep their own capacity.
	files, err := testMultiple.List(t.Context(), m, reader)
	if err != nil || len(files) != 1 {
		t.Fatal("read waited for write capacity", err)
	}
	if body, err := testMultiple.ReadBytes(t.Context(), m, reader, files[0].ID(), 1024); err != nil || string(body) != "readable" {
		t.Fatal("attachment read waited for write capacity", err)
	}
	// A further write queues briefly, then fails as retryable overload.
	busy, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := testSingle.Add(busy, m, member(t, 3), uploadText("queued")); !errors.Is(err, fault.Overloaded) {
		t.Fatal("exhausted write capacity was not an overload", err)
	}
	// Owner-delete observers never fail because application writes are busy.
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=2`); err != nil {
			return err
		}
		return Cleanup(ctx, tx, m, extensiontest.Members, deleted, lifecycle.Delete, nil)
	})
	if err != nil {
		t.Fatal("owner cleanup competed with write capacity", err)
	}
}
