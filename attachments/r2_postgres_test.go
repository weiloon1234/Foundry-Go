package attachments

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// r2AttachmentBackend retains real local bytes while refusing conditional
// deletes, just as the R2 profile does. It also records namespace confinement.
type r2AttachmentBackend struct {
	storage.Backend
	mu       sync.Mutex
	old      map[storage.ObjectKey]bool
	lists    []storage.ListOptions
	deletes  []storage.DeleteOptions
	noCreate bool
	noRead   bool
}

func (b *r2AttachmentBackend) Capabilities() storage.Capabilities {
	c := b.Backend.Capabilities()
	c.ConditionalDelete = false
	c.ConditionalCreate = !b.noCreate
	c.ConditionalRead = !b.noRead
	return c
}
func (b *r2AttachmentBackend) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	if options.Condition != storage.IfAbsent() {
		return storage.ObjectInfo{}, errors.New("attachment key was not created with IfAbsent")
	}
	return b.Backend.Put(ctx, key, source, options)
}
func (b *r2AttachmentBackend) Delete(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
	if options.IfMatch != "" || options.Version != "" {
		return storage.Failure(storage.Unsupported, storage.DeleteOperation, storage.Unchanged, nil)
	}
	b.mu.Lock()
	b.deletes = append(b.deletes, options)
	b.mu.Unlock()
	return b.Backend.Delete(ctx, key, options)
}
func (b *r2AttachmentBackend) List(ctx context.Context, options storage.ListOptions) (storage.Page, error) {
	page, err := b.Backend.List(ctx, options)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lists = append(b.lists, options)
	for i := range page.Objects {
		if b.old[page.Objects[i].Key] {
			page.Objects[i].Modified, _ = temporal.NewDateTime(time.Now().Add(-48 * time.Hour))
		}
	}
	return page, err
}
func (b *r2AttachmentBackend) Stat(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (storage.ObjectInfo, error) {
	info, err := b.Backend.Stat(ctx, key, options)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.old[key] {
		info.Modified, _ = temporal.NewDateTime(time.Now().Add(-48 * time.Hour))
	}
	return info, err
}
func (b *r2AttachmentBackend) age(keys ...storage.ObjectKey) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.old == nil {
		b.old = make(map[storage.ObjectKey]bool)
	}
	for _, key := range keys {
		b.old[key] = true
	}
}
func openR2Attachments(t *testing.T, extra ...Registration) (attachmentFixture, *r2AttachmentBackend) {
	t.Helper()
	var backend *r2AttachmentBackend
	f := openAttachmentsOn(t, nil, func(inner storage.Backend) storage.Backend {
		backend = &r2AttachmentBackend{Backend: inner}
		return backend
	}, extra...)
	f.backend.setDelete(func(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
		// Read through a separate transaction: an uncommitted cleanup transition
		// must never authorize storage deletion.
		if err := f.Store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			var live int64
			if err := database.ScanOne(ctx, tx, `SELECT
    (SELECT count(*) FROM foundry_attachments WHERE object_key=$1 AND state NOT IN ('cleanup','cleaned')) +
    (SELECT count(*) FROM foundry_attachment_variants WHERE object_key=$1 AND state NOT IN ('cleanup','cleaned'))`, []any{key.String()}, &live); err != nil {
				return err
			}
			if live != 0 {
				return errors.New("delete preceded durable retirement")
			}
			return nil
		}); err != nil {
			return err
		}
		return f.backend.Backend.Delete(ctx, key, options)
	})
	return f, backend
}
func peerAttachments(t *testing.T, f attachmentFixture, registrations ...Registration) *Manager {
	t.Helper()
	m, err := New(Dependencies{Store: f.Store, Disks: f.registry, Image: f.image, Locales: f.locales}, DefaultConfig(), registrations...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m
}
func ageAttachmentIntents(ctx context.Context, f attachmentFixture) error {
	return f.Store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE foundry_attachments SET updated_at = updated_at - interval '2 hours' WHERE state IN ('writing', 'uncertain', 'stored')`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE foundry_attachment_variants SET updated_at = updated_at - interval '2 hours' WHERE state IN ('writing', 'uncertain')`)
		return err
	})
}
func assertObjectAbsent(t *testing.T, disk *storage.Disk, key storage.ObjectKey) {
	t.Helper()
	if _, err := disk.Stat(t.Context(), key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
		t.Fatal("retired object remains", err)
	}
}

func TestPostgresR2AttachmentReplacementAndInterruptedPublication(t *testing.T) {
	f, backend := openR2Attachments(t)
	owner := member(t, 1)
	first := addText(t, f, testSingle, owner, "first")
	second := addText(t, f, testSingle, owner, "second")
	if journal(t, f, first.Operation).State != string(Cleaned) {
		t.Fatal("old row was not cleaned")
	}
	assertObjectAbsent(t, f.disk, attachmentOf(t, first).key)
	assertBody(t, f, testSingle, owner, attachmentOf(t, second), "second")
	// Applied with a lost acknowledgement is recovered, never published.
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		if _, err := f.backend.Backend.Put(ctx, key, source, options); err != nil {
			return storage.ObjectInfo{}, err
		}
		return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
	})
	lost, err := testSingle.Add(t.Context(), f.manager, owner, uploadText("lost"))
	f.backend.setPut(nil)
	if err == nil || lost.Publication != Unpublished || journal(t, f, lost.Operation).State != string(Uncertain) {
		t.Fatal("interrupted publication adopted", err)
	}
	if err := ageAttachmentIntents(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ReconcilePending(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	row := journal(t, f, lost.Operation)
	if row.State != string(Cleaned) {
		t.Fatal("interrupted publication not cleaned", row.State)
	}
	key, _ := storage.ParseKey(row.ObjectKey)
	assertObjectAbsent(t, f.disk, key)
	assertBody(t, f, testSingle, owner, attachmentOf(t, second), "second")
	if len(backend.deletes) < 2 {
		t.Fatal("write-once cleanup did not delete")
	}
}

func TestPostgresR2AttachmentConcurrentReplacement(t *testing.T) {
	f, _ := openR2Attachments(t)
	owner := member(t, 1)
	first := addText(t, f, testSingle, owner, "first")
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		info, err := f.backend.Backend.Put(ctx, key, source, options)
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return info, ctx.Err()
		}
		return info, err
	})
	type outcome struct {
		result Result[extensiontest.Member, int64]
		err    error
	}
	done := make(chan outcome, 2)
	for _, body := range []string{"second", "third"} {
		go func() {
			result, err := testSingle.Add(t.Context(), f.manager, owner, uploadText(body))
			done <- outcome{result, err}
		}()
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			close(release)
			t.Fatal("writers did not reach storage")
		}
	}
	close(release)
	results := []OperationID{first.Operation}
	for range 2 {
		result := <-done
		if result.err != nil {
			t.Fatal(result.err)
		}
		results = append(results, result.result.Operation)
	}
	files, err := testSingle.List(t.Context(), f.manager, owner)
	if err != nil || len(files) != 1 {
		t.Fatal("concurrent replacement lost cardinality", err)
	}
	for _, id := range results {
		row := journal(t, f, id)
		key, _ := storage.ParseKey(row.ObjectKey)
		if key == files[0].key {
			if row.State != string(Ready) {
				t.Fatal("winning row not ready")
			}
			if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
				t.Fatal("winning key deleted", err)
			}
		} else {
			if row.State != string(Cleaned) {
				t.Fatal("losing row not cleaned", row.State)
			}
			assertObjectAbsent(t, f.disk, key)
		}
	}
}

func TestPostgresR2AttachmentStaleWriterFencedAfterPut(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		lostAck, settling bool
	}{
		{name: "acknowledged"}, {name: "lost_acknowledgement", lostAck: true}, {name: "settlement_in_progress", settling: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, _ := openR2Attachments(t)
			peer := peerAttachments(t, f, testSingle.Registration())
			f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
				if err := ageAttachmentIntents(ctx, f); err != nil {
					return storage.ObjectInfo{}, err
				}
				if scenario.settling {
					if err := f.Store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
						_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET state='uncertain', writer_id=$1 WHERE object_key=$2`, peer.writer.String(), key.String())
						return err
					}); err != nil {
						return storage.ObjectInfo{}, err
					}
				} else if _, err := peer.ReconcilePending(ctx, 10); err != nil {
					return storage.ObjectInfo{}, err
				}
				info, err := f.backend.Backend.Put(ctx, key, source, options)
				if err == nil && scenario.lostAck {
					err = storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
				}
				return info, err
			})
			result, err := testSingle.Add(t.Context(), f.manager, member(t, 1), uploadText("late"))
			if err == nil || result.Publication != Unpublished {
				t.Fatal("stale writer published", err)
			}
			row := journal(t, f, result.Operation)
			if row.State != string(Cleaned) {
				t.Fatal("tombstone resurrected", row.State)
			}
			key, _ := storage.ParseKey(row.ObjectKey)
			assertObjectAbsent(t, f.disk, key)
		})
	}
}

func TestPostgresR2AttachmentVariantRegenerationAndStaleWriter(t *testing.T) {
	f, backend := openR2Attachments(t, testPhotos.Registration())
	first, err := testPhotos.Add(t.Context(), f.manager, member(t, 1), pngUpload(t, 16, 8))
	if err != nil {
		t.Fatal(err)
	}
	old := variantRows(t, f, first.Operation)
	for _, row := range old {
		key, _ := storage.ParseKey(row.ObjectKey)
		backend.age(key)
	}
	inspection, err := f.manager.InspectStorage(t.Context(), StorageInspection{Disk: testDisk, Owner: extensiontest.Members.Name(), MinimumAge: time.Minute, Limit: 100})
	if err != nil || len(inspection.Candidates) != 0 {
		t.Fatal("live variants reported as untracked", err)
	}
	page, err := testPhotos.RegenerateVariants(t.Context(), f.manager, RegenerationOptions{Limit: 10})
	if err != nil || len(page.Regenerated) != 1 || len(page.Failed) != 0 {
		t.Fatal("regeneration failed", err)
	}
	current := variantRows(t, f, first.Operation)
	if len(current) != 2*len(old) {
		t.Fatal("generation reused a row")
	}
	for _, prior := range old {
		key, _ := storage.ParseKey(prior.ObjectKey)
		assertObjectAbsent(t, f.disk, key)
		for _, row := range current {
			if row.State == string(VariantReady) && row.ObjectKey == prior.ObjectKey {
				t.Fatal("generation reused a key")
			}
		}
	}
	peer := peerAttachments(t, f, testPhotos.Registration())
	var late storage.ObjectKey
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		if late.IsZero() && strings.Contains(key.String(), "/variants/") {
			late = key
			if err := ageAttachmentIntents(ctx, f); err != nil {
				return storage.ObjectInfo{}, err
			}
			if _, err := peer.ReconcilePending(ctx, 20); err != nil {
				return storage.ObjectInfo{}, err
			}
		}
		return f.backend.Backend.Put(ctx, key, source, options)
	})
	page, err = testPhotos.RegenerateVariants(t.Context(), f.manager, RegenerationOptions{Limit: 10})
	f.backend.setPut(nil)
	if err != nil || len(page.Failed) != 1 || late.IsZero() {
		t.Fatal("stale variant was adopted", err)
	}
	assertObjectAbsent(t, f.disk, late)
	if _, err := testPhotos.Clear(t.Context(), f.manager, member(t, 1)); err != nil {
		t.Fatal(err)
	}
	assertVariantsCleaned(t, f, f.disk, first.Operation)
}

func TestPostgresR2AttachmentOrphanSweepScopeGraceAndProgress(t *testing.T) {
	f, backend := openR2Attachments(t)
	owner := member(t, 1)
	live := attachmentOf(t, addText(t, f, testSingle, owner, "live"))
	kept := attachmentOf(t, addText(t, f, testMultiple, owner, "retained"))
	if _, err := testMultiple.DetachKeepFile(t.Context(), f.manager, owner, kept.ID()); err != nil {
		t.Fatal(err)
	}
	protected := []storage.ObjectKey{live.key, kept.key}
	for _, state := range []State{Stored, Writing, Uncertain} {
		prepared, err := testSingle.Prepare(t.Context(), f.manager, owner, uploadText(string(state)))
		if err != nil {
			t.Fatal(err)
		}
		row := journal(t, f, prepared.Operation())
		key, _ := storage.ParseKey(row.ObjectKey)
		protected = append(protected, key)
		if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET state=$1 WHERE id=$2`, string(state), row.ID.String())
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Return Unknown without submitting, then simulate a provider completing
	// after settlement marked the absent key Cleaned.
	var lateKey storage.ObjectKey
	f.backend.setPut(func(_ context.Context, key storage.ObjectKey, _ io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
		lateKey = key
		return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
	})
	lost, err := testSingle.Add(t.Context(), f.manager, owner, uploadText("late"))
	f.backend.setPut(nil)
	if err == nil {
		t.Fatal("expected unresolved put")
	}
	if _, err := f.manager.Settle(t.Context(), lost.Operation, WriterStoppedAndStorageSettled, nil); err != nil {
		t.Fatal(err)
	}
	newKey := func(scope string) storage.ObjectKey {
		t.Helper()
		id, err := model.NewIDAt[store.File](time.Now())
		if err != nil {
			t.Fatal(err)
		}
		key, err := objectKey(scope, id)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	orphan := newKey(extensiontest.Members.Scope())
	variantID, _ := model.NewIDAt[store.Variant](time.Now())
	variantOrphan, _ := variantKey(extensiontest.Members.Scope(), variantID)
	outside := newKey("unregistered-scope")
	foreign, _ := storage.ParseKey(objectPrefix + extensiontest.Members.Scope() + "/not-an-attachment")
	for _, key := range []storage.ObjectKey{lateKey, orphan, variantOrphan, outside, foreign} {
		if _, err := f.disk.PutBytes(t.Context(), key, []byte("late"), storage.PutOptions{Condition: storage.IfAbsent()}); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		if _, err := f.manager.ReconcilePending(t.Context(), 100); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.disk.Stat(t.Context(), lateKey, storage.ReadOptions{}); err != nil {
		t.Fatal("grace period ignored", err)
	}
	backend.age(append(protected, lateKey, orphan, variantOrphan, outside, foreign)...)
	// A one-object page must eventually progress beyond preserved live rows.
	for range 40 {
		if _, err := f.manager.ReconcilePending(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
	}
	assertObjectAbsent(t, f.disk, lateKey)
	for _, key := range append(protected, orphan, variantOrphan, outside, foreign) {
		if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
			t.Fatal("sweep removed protected object", err)
		}
	}
	inspection, err := f.manager.InspectStorage(t.Context(), StorageInspection{Disk: testDisk, Owner: extensiontest.Members.Name(), MinimumAge: time.Minute, Limit: 100})
	if err != nil || inspection.Unknown != 3 || len(inspection.Candidates) != 3 {
		t.Fatal("unknown objects were not reported", inspection.Unknown, err)
	}
	unknown := make(map[storage.ObjectKey]bool)
	for _, candidate := range inspection.Candidates {
		unknown[candidate.Key] = true
	}
	for _, key := range []storage.ObjectKey{orphan, variantOrphan, foreign} {
		if !unknown[key] {
			t.Fatal("unknown key missing from inspection", key)
		}
	}
	for _, options := range backend.lists {
		if options.Prefix.String() != objectPrefix+extensiontest.Members.Scope()+"/" && options.Prefix.String() != objectPrefix+extensiontest.Others.Scope()+"/" {
			t.Fatal("sweep escaped registered scope")
		}
		if options.Limit != 1 && options.Limit != 100 {
			t.Fatal("unbounded listing")
		}
	}
}

func TestPostgresAttachmentRequiresCreateAndReadAndKeepsPinnedDelete(t *testing.T) {
	f := openAttachments(t)
	for _, noCreate := range []bool{true, false} {
		backend := &r2AttachmentBackend{Backend: f.backend.Backend, noCreate: noCreate, noRead: !noCreate}
		disk, err := testDisk.Bind(backend, storage.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		registry, err := storage.NewRegistry(disk)
		if err != nil {
			t.Fatal(err)
		}
		_, err = New(Dependencies{Store: f.Store, Disks: registry}, DefaultConfig(), testSingle.Registration())
		if !errors.Is(err, storage.Unsupported) {
			t.Fatal("missing required capability accepted", err)
		}
		if err := disk.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	// The versioned adapter enforces the AWS profile's version-only delete rule.
	aws := openAttachmentsOn(t, nil, newVersionedBackend)
	first := addText(t, aws, testSingle, member(t, 1), "first")
	addText(t, aws, testSingle, member(t, 1), "second")
	selected := aws.backend.Backend.(*versionedBackend).deletes()
	if len(selected) != 1 || selected[0].Version == "" || selected[0].IfMatch != "" {
		t.Fatal("AWS immutable version pin lost")
	}
	assertObjectAbsent(t, aws.disk, attachmentOf(t, first).key)
	var conditional storage.DeleteOptions
	f.backend.setDelete(func(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
		conditional = options
		return f.backend.Backend.Delete(ctx, key, options)
	})
	addText(t, f, testSingle, member(t, 1), "first")
	addText(t, f, testSingle, member(t, 1), "second")
	if conditional.IfMatch == "" || conditional.Version != "" {
		t.Fatal("unversioned conditional delete pin lost")
	}
}

func TestPostgresAttachmentLegacyVariantKeysRemainReadableAndCleanable(t *testing.T) {
	f := openAttachments(t, testPhotos.Registration())
	result, err := testPhotos.Add(t.Context(), f.manager, member(t, 1), pngUpload(t, 16, 8))
	if err != nil {
		t.Fatal(err)
	}
	var legacy []storage.ObjectKey
	for _, row := range variantRows(t, f, result.Operation) {
		key, _ := storage.ParseKey(row.ObjectKey)
		body, _, err := f.disk.ReadBytes(t.Context(), key, MaxUploadBytes, storage.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		oldKey, _ := storage.ParseKey(legacyVariantPrefix + extensiontest.Members.Scope() + "/" + row.ID.String())
		stored, err := f.disk.PutBytes(t.Context(), oldKey, body, storage.PutOptions{ContentType: storage.MediaType(row.ContentType), Condition: storage.IfAbsent()})
		if err != nil {
			t.Fatal(err)
		}
		// Reconstruct the persisted layout from before the scope-prefix change.
		if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE foundry_attachment_variants SET object_key=$1, etag=$2 WHERE id=$3`, oldKey.String(), string(stored.Object.ETag), row.ID.String())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := f.disk.Delete(t.Context(), key, pinnedDelete(row.ObjectVersion, row.ETag)); err != nil {
			t.Fatal(err)
		}
		legacy = append(legacy, oldKey)
	}
	files, err := testPhotos.List(t.Context(), f.manager, member(t, 1))
	if err != nil || len(files) != 1 || len(files[0].variants) != len(legacy) {
		t.Fatal("legacy variants did not load", err)
	}
	for _, variant := range files[0].variants {
		if !strings.HasPrefix(variant.key.String(), legacyVariantPrefix) {
			t.Fatal("legacy key changed on read")
		}
		if _, _, err := f.disk.ReadBytes(t.Context(), variant.key, MaxUploadBytes, storage.ReadOptions{IfMatch: variant.etag}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := testPhotos.RegenerateVariants(t.Context(), f.manager, RegenerationOptions{Limit: 10})
	if err != nil || len(page.Regenerated) != 1 || len(page.Failed) != 0 {
		t.Fatal("legacy regeneration failed", err)
	}
	for _, key := range legacy {
		assertObjectAbsent(t, f.disk, key)
	}
	for _, row := range variantRows(t, f, result.Operation) {
		if row.State == string(VariantReady) && !strings.HasPrefix(row.ObjectKey, objectPrefix+extensiontest.Members.Scope()+"/variants/") {
			t.Fatal("regeneration retained legacy prefix")
		}
	}
}
