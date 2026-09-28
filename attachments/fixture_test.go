package attachments

import (
	"context"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

// Real local storage supplies conditional identity/integrity. Only the selected
// provider boundary is injected; database transactions remain real PostgreSQL.
type controlledBackend struct {
	storage.Backend
	mu     sync.Mutex
	put    func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error)
	delete func(context.Context, storage.ObjectKey, storage.DeleteOptions) error
}

func (b *controlledBackend) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	b.mu.Lock()
	fn := b.put
	b.mu.Unlock()
	if fn != nil {
		return fn(ctx, key, source, options)
	}
	return b.Backend.Put(ctx, key, source, options)
}
func (b *controlledBackend) Delete(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
	b.mu.Lock()
	fn := b.delete
	b.mu.Unlock()
	if fn != nil {
		return fn(ctx, key, options)
	}
	return b.Backend.Delete(ctx, key, options)
}
func (b *controlledBackend) setPut(fn func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error)) {
	b.mu.Lock()
	b.put = fn
	b.mu.Unlock()
}
func (b *controlledBackend) setDelete(fn func(context.Context, storage.ObjectKey, storage.DeleteOptions) error) {
	b.mu.Lock()
	b.delete = fn
	b.mu.Unlock()
}

type attachmentFixture struct {
	extensiontest.Fixture
	manager  *Manager
	disk     *storage.Disk
	backend  *controlledBackend
	registry *storage.Registry
	image    *imaging.Engine
	locales  i18n.LocaleSet
}

func openAttachments(t *testing.T, extra ...Registration) attachmentFixture {
	t.Helper()
	return openAttachmentsWithConnector(t, nil, extra...)
}
func openAttachmentsWithConnector(t *testing.T, wrap func(driver.Connector) driver.Connector, extra ...Registration) attachmentFixture {
	t.Helper()
	f := attachmentFixture{Fixture: extensiontest.OpenWithConnector(t, Migrations(), wrap)}
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	f.backend = &controlledBackend{Backend: backend}
	f.disk, err = testDisk.Bind(f.backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.disk.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	f.registry, err = storage.NewRegistry(f.disk)
	if err != nil {
		t.Fatal(err)
	}
	f.image, err = imaging.New(imaging.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.image.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	f.locales, err = i18n.NewLocaleSet("en", "en", "fr", "ms")
	if err != nil {
		t.Fatal(err)
	}
	registrations := []Registration{testSingle.Registration(), testMultiple.Registration(), testLocalized.Registration(), testOther.Registration()}
	registrations = append(registrations, extra...)
	f.manager, err = New(Dependencies{Store: f.Store, Disks: f.registry, Image: f.image, Locales: f.locales}, DefaultConfig(), registrations...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return f
}
func member(t *testing.T, id int64) model.Reference[extensiontest.Member, int64] {
	t.Helper()
	return extensiontest.Members.Reference(id)
}
func uploadText(text string) Upload {
	return Upload{Source: strings.NewReader(text), OriginalName: "note.txt"}
}
func addText(t *testing.T, f attachmentFixture, collection Collection[extensiontest.Member, int64], owner model.Reference[extensiontest.Member, int64], text string) Result[extensiontest.Member, int64] {
	t.Helper()
	result, err := collection.Add(t.Context(), f.manager, owner, uploadText(text))
	if err != nil || result.Publication != Published || !result.Attachment.IsSet() {
		t.Fatal("upload failed", err)
	}
	return result
}
func attachmentOf(t *testing.T, result Result[extensiontest.Member, int64]) Attachment[extensiontest.Member, int64] {
	t.Helper()
	file, ok := result.Attachment.Get()
	if !ok {
		t.Fatal("missing published attachment")
	}
	return file
}
func journal(t *testing.T, f attachmentFixture, id OperationID) store.File {
	t.Helper()
	var row store.File
	err := f.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		var err error
		row, err = store.QueryFoundryAttachments().RequireFind(ctx, tx, fileID(id))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}
func assertBody(t *testing.T, f attachmentFixture, collection Collection[extensiontest.Member, int64], owner model.Reference[extensiontest.Member, int64], file Attachment[extensiontest.Member, int64], want string) {
	t.Helper()
	got, err := collection.ReadBytes(t.Context(), f.manager, owner, file.ID(), 1024)
	if err != nil || string(got) != want {
		t.Fatal("attachment bytes differ", err)
	}
}
