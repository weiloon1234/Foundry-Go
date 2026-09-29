package attachments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
)

// versionedBackend emulates a versioned bucket over real local storage: each
// publication receives a new immutable version ID, and, as on AWS, a delete
// condition cannot be combined with a version selector.
type versionedBackend struct {
	storage.Backend
	mu       sync.Mutex
	next     int
	current  map[storage.ObjectKey]storage.VersionID
	selected []storage.DeleteOptions
}

func newVersionedBackend(inner storage.Backend) storage.Backend {
	return &versionedBackend{Backend: inner, current: make(map[storage.ObjectKey]storage.VersionID)}
}
func (b *versionedBackend) Capabilities() storage.Capabilities {
	capabilities := b.Backend.Capabilities()
	capabilities.Versions = true
	return capabilities
}
func (b *versionedBackend) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	info, err := b.Backend.Put(ctx, key, source, options)
	if err != nil {
		return info, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	info.Version = storage.VersionID(fmt.Sprintf("version-%d", b.next))
	b.current[key] = info.Version
	return info, nil
}

// pin maps a version selector onto the current local object.
func (b *versionedBackend) pin(key storage.ObjectKey, version storage.VersionID) (storage.VersionID, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current, ok := b.current[key]
	if version != "" && (!ok || version != current) {
		return "", storage.Failure(storage.NotFound, storage.StatOperation, storage.NotApplicable, nil)
	}
	return current, nil
}
func (b *versionedBackend) Stat(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (storage.ObjectInfo, error) {
	current, err := b.pin(key, options.Version)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	options.Version = ""
	info, err := b.Backend.Stat(ctx, key, options)
	info.Version = current
	return info, err
}
func (b *versionedBackend) Open(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	current, err := b.pin(key, options.Version)
	if err != nil {
		return nil, storage.ReadInfo{}, err
	}
	options.Version = ""
	body, info, err := b.Backend.Open(ctx, key, options)
	info.Object.Version = current
	return body, info, err
}
func (b *versionedBackend) Delete(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
	b.mu.Lock()
	b.selected = append(b.selected, options)
	b.mu.Unlock()
	if options.IfMatch != "" && options.Version != "" {
		return storage.Failure(storage.Unsupported, storage.DeleteOperation, storage.Unchanged, nil)
	}
	if options.Version == "" {
		return b.Backend.Delete(ctx, key, options)
	}
	if _, err := b.pin(key, options.Version); err != nil {
		// Deleting an absent version is idempotent.
		return nil
	}
	options.Version = ""
	if err := b.Backend.Delete(ctx, key, options); err != nil {
		return err
	}
	b.mu.Lock()
	delete(b.current, key)
	b.mu.Unlock()
	return nil
}
func (b *versionedBackend) deletes() []storage.DeleteOptions {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]storage.DeleteOptions(nil), b.selected...)
}

func TestPostgresAttachmentCleanupOnVersionedBucketDeletesByVersion(t *testing.T) {
	var versioned *versionedBackend
	f := openAttachmentsOn(t, nil, func(inner storage.Backend) storage.Backend {
		versioned = newVersionedBackend(inner).(*versionedBackend)
		return versioned
	})
	owner := member(t, 1)
	first := addText(t, f, testSingle, owner, "first version")
	old := attachmentOf(t, first)
	if old.version == "" {
		t.Fatal("versioned publication did not retain its version")
	}
	assertBody(t, f, testSingle, owner, old, "first version")
	replaced, err := testSingle.Replace(t.Context(), f.manager, owner, uploadText("second version"))
	if err != nil || replaced.Publication != Published || len(replaced.PendingCleanup) != 0 {
		t.Fatal("replacement cleanup failed on a versioned bucket", err)
	}
	if state := journal(t, f, first.Operation); state.State != string(Cleaned) {
		t.Fatal("replaced object was not cleaned", state.State, state.LastFailure)
	}
	if _, err := f.disk.Stat(t.Context(), old.key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
		t.Fatal("replaced object retained", err)
	}
	current := attachmentOf(t, replaced)
	change, err := testSingle.Detach(t.Context(), f.manager, owner, current.ID())
	if err != nil {
		t.Fatal("detach cleanup failed on a versioned bucket", err, change)
	}
	if state := journal(t, f, replaced.Operation); state.State != string(Cleaned) {
		t.Fatal("detached object was not cleaned", state.State)
	}
	deletes := versioned.deletes()
	if len(deletes) != 2 {
		t.Fatal("unexpected cleanup deletes", len(deletes))
	}
	for i, options := range deletes {
		if options.Version == "" || options.IfMatch != "" {
			t.Fatal("cleanup did not delete exactly the pinned version", i, options)
		}
	}
}
