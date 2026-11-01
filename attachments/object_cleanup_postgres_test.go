package attachments

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func cleanupManagerAt(t *testing.T, f attachmentFixture, now time.Time, grace time.Duration, registrations ...Registration) *Manager {
	t.Helper()
	source := inspectionClock{now: now}
	connection := pgtest.Config(t)
	connection.Schema = f.Schema
	db, err := postgres.Open(t.Context(), connection, database.WithClock(source))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	settings := extensions.DefaultConfig()
	settings.Schema = f.Schema
	settings.Clock = source
	store, err := extensions.New(db, f.Store.Registry(), settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	config := DefaultConfig()
	config.CleanupGrace = grace
	manager, err := New(Dependencies{Store: store, Disks: f.registry, Image: f.image, Locales: f.locales}, config, registrations...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return manager
}

func TestPostgresAttachmentSweepPreservesSharedNamespaceAndAliases(t *testing.T) {
	for _, scenario := range []struct {
		name                 string
		otherDatabase, alias bool
	}{
		{name: "other_database", otherDatabase: true},
		{name: "disk_alias", alias: true},
		{name: "other_database_and_alias", otherDatabase: true, alias: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, backend := openR2Attachments(t)
			upload := addText(t, f, testSingle, member(t, 1), "another owner's bytes")
			file := attachmentOf(t, upload)
			backend.age(file.key)
			visitor := f
			// Separate PostgreSQL schemas are independent attachment databases:
			// both contain the same owner declarations and object scope prefix.
			if scenario.otherDatabase {
				visitor.Fixture = extensiontest.Open(t, Migrations())
			}
			disk := testDisk
			if scenario.alias {
				disk = storage.DefineDisk("alias")
				bound, err := disk.Bind(f.backend, storage.DefaultConfig())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := bound.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				visitor.registry, err = storage.NewRegistry(bound)
				if err != nil {
					t.Fatal(err)
				}
			}
			collection := Define(extensiontest.Members, "shared", Policy{Disk: disk, Cardinality: Single, AnyMedia: true})
			visitor.manager = peerAttachments(t, visitor, collection.Registration())
			for range 3 {
				if _, err := visitor.manager.ReconcilePending(t.Context(), 10); err != nil {
					t.Fatal(err)
				}
			}
			page, err := visitor.manager.InspectStorage(t.Context(), StorageInspection{Disk: disk, Owner: extensiontest.Members.Name(), MinimumAge: time.Minute, Limit: 10})
			if err != nil || page.Unknown != 1 || len(page.Candidates) != 1 || page.Candidates[0].Key != file.key {
				t.Fatal("foreign key not reported as unknown", page.Unknown, err)
			}
			assertBody(t, f, testSingle, member(t, 1), file, "another owner's bytes")
			if journal(t, f, upload.Operation).State != string(Ready) || len(backend.deletes) != 0 {
				t.Fatal("visitor modified another owner's attachment")
			}
		})
	}
}

func TestPostgresAttachmentReplacementCleanupWaitsGrace(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	now := time.Now().UTC()
	grace := 72 * time.Hour
	registrations := []Registration{testSingle.Registration()}
	f.manager = cleanupManagerAt(t, f, now, grace, registrations...)
	first := addText(t, f, testSingle, owner, "old")
	old := attachmentOf(t, first)
	// An old object still needs a full grace starting at replacement.
	retiredAt := now.Add(7 * 24 * time.Hour)
	f.manager = cleanupManagerAt(t, f, retiredAt, grace, registrations...)
	replacement, err := testSingle.Replace(t.Context(), f.manager, owner, uploadText("new"))
	if err != nil || replacement.Publication != Published || len(replacement.PendingCleanup) != 1 || replacement.PendingCleanup[0] != first.Operation {
		t.Fatal("replacement did not report deferred cleanup", err)
	}
	for _, at := range []time.Time{retiredAt, retiredAt.Add(grace - time.Second)} {
		manager := cleanupManagerAt(t, f, at, grace, registrations...)
		result, err := manager.Reconcile(t.Context(), first.Operation)
		if err != nil || result.State != CleanupPending || result.Attempts != 0 {
			t.Fatal("grace wait changed cleanup status", result, err)
		}
		if _, err := manager.ReconcilePending(t.Context(), 10); err != nil {
			t.Fatal(err)
		}
		if _, err := f.disk.Stat(t.Context(), old.key, storage.ReadOptions{}); err != nil {
			t.Fatal("replacement deleted before grace", err)
		}
	}
	manager := cleanupManagerAt(t, f, retiredAt.Add(grace), grace, registrations...)
	if _, err := manager.ReconcilePending(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	assertObjectAbsent(t, f.disk, old.key)
	if journal(t, f, first.Operation).State != string(Cleaned) {
		t.Fatal("mature replacement remained pending")
	}
	assertBody(t, f, testSingle, owner, attachmentOf(t, replacement), "new")
}

func TestPostgresAttachmentVariantCleanupWaitsGrace(t *testing.T) {
	f, _ := openR2Attachments(t, testPhotos.Registration())
	now := time.Now().UTC()
	grace := 72 * time.Hour
	f.manager = cleanupManagerAt(t, f, now, grace, testPhotos.Registration())
	first, err := testPhotos.Add(t.Context(), f.manager, member(t, 1), pngUpload(t, 16, 8))
	if err != nil {
		t.Fatal(err)
	}
	old := variantRows(t, f, first.Operation)
	page, err := testPhotos.RegenerateVariants(t.Context(), f.manager, RegenerationOptions{Limit: 10})
	if err != nil || len(page.Regenerated) != 1 || len(page.Failed) != 0 {
		t.Fatal("regeneration treated grace as a failure", err)
	}
	for _, row := range old {
		key, _ := storage.ParseKey(row.ObjectKey)
		if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
			t.Fatal("old variant deleted before grace", err)
		}
	}
	change, err := testPhotos.Clear(t.Context(), f.manager, member(t, 1))
	if err != nil || len(change.PendingCleanup) != 1 {
		t.Fatal("original not pending while variants wait", err)
	}
	before := cleanupManagerAt(t, f, now.Add(grace-time.Second), grace, testPhotos.Registration())
	if _, err := before.ReconcilePending(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	for _, row := range variantRows(t, f, first.Operation) {
		key, _ := storage.ParseKey(row.ObjectKey)
		if row.State != string(VariantCleanup) || row.Attempts != 0 {
			t.Fatal("variant grace changed journal", row.State, row.Attempts)
		}
		if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
			t.Fatal("variant deleted before grace", err)
		}
	}
	manager := cleanupManagerAt(t, f, now.Add(grace), grace, testPhotos.Registration())
	result, err := manager.Reconcile(t.Context(), first.Operation)
	if err != nil || result.State != Cleaned {
		t.Fatal("original cleanup did not finish with its variants", result, err)
	}
	assertVariantsCleaned(t, f, f.disk, first.Operation)
	assertObjectAbsent(t, f.disk, attachmentOf(t, first).key)
}

func TestPostgresAttachmentStaleWriterTombstoneWaitsGrace(t *testing.T) {
	for _, variant := range []bool{false, true} {
		name := "original"
		if variant {
			name = "variant"
		}
		t.Run(name, func(t *testing.T) {
			f, backend := openR2Attachments(t, testPhotos.Registration())
			now := time.Now().UTC()
			grace := 72 * time.Hour
			registrations := []Registration{testSingle.Registration(), testPhotos.Registration()}
			f.manager = cleanupManagerAt(t, f, now, grace, registrations...)
			peer := cleanupManagerAt(t, f, now, grace, registrations...)
			var late storage.ObjectKey
			f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
				if late.IsZero() && strings.Contains(key.String(), "/variants/") == variant {
					late = key
					if err := ageAttachmentIntents(ctx, f); err != nil {
						return storage.ObjectInfo{}, err
					}
					if _, err := peer.ReconcilePending(ctx, 10); err != nil {
						return storage.ObjectInfo{}, err
					}
				}
				return f.backend.Backend.Put(ctx, key, source, options)
			})
			if variant {
				result, err := testPhotos.Add(t.Context(), f.manager, member(t, 1), pngUpload(t, 16, 8))
				if err == nil || result.Publication != Published || !result.PendingVariants {
					t.Fatal("stale variant unexpectedly published", err)
				}
			} else {
				result, err := testSingle.Add(t.Context(), f.manager, member(t, 1), uploadText("late"))
				if err == nil || result.Publication != Unpublished || journal(t, f, result.Operation).State != string(Cleaned) {
					t.Fatal("stale writer unexpectedly published", err)
				}
			}
			f.backend.setPut(nil)
			if late.IsZero() {
				t.Fatal("writer was not fenced")
			}
			backend.age(late)
			for _, at := range []time.Time{now, now.Add(grace - time.Second)} {
				manager := cleanupManagerAt(t, f, at, grace, registrations...)
				if _, err := manager.ReconcilePending(t.Context(), 10); err != nil {
					t.Fatal(err)
				}
				if _, err := f.disk.Stat(t.Context(), late, storage.ReadOptions{}); err != nil {
					t.Fatal("stale object deleted before tombstone grace", err)
				}
			}
			manager := cleanupManagerAt(t, f, now.Add(grace), grace, registrations...)
			if _, err := manager.ReconcilePending(t.Context(), 10); err != nil {
				t.Fatal(err)
			}
			assertObjectAbsent(t, f.disk, late)
		})
	}
}

func TestAttachmentCleanupGraceConfiguration(t *testing.T) {
	config := DefaultConfig()
	if config.CleanupGrace != 0 {
		t.Fatal("development cleanup must default to zero grace")
	}
	for _, grace := range []time.Duration{0, time.Second, 72 * time.Hour, 30 * 24 * time.Hour} {
		config.CleanupGrace = grace
		if err := config.Validate(); err != nil {
			t.Fatal("valid cleanup grace rejected", grace, err)
		}
	}
	config.CleanupGrace = -time.Second
	if config.Validate() == nil {
		t.Fatal("negative cleanup grace accepted")
	}
}

func TestPostgresAttachmentSettlementCleanupWaitsGrace(t *testing.T) {
	f := openAttachments(t)
	now := time.Now().UTC()
	grace := 72 * time.Hour
	f.manager = cleanupManagerAt(t, f, now, grace, testSingle.Registration())
	var key storage.ObjectKey
	f.backend.setPut(func(ctx context.Context, target storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		key = target
		if _, err := f.backend.Backend.Put(ctx, target, source, options); err != nil {
			return storage.ObjectInfo{}, err
		}
		return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
	})
	upload, err := testSingle.Add(t.Context(), f.manager, member(t, 1), uploadText("lost acknowledgement"))
	f.backend.setPut(nil)
	if err == nil || journal(t, f, upload.Operation).State != string(Uncertain) {
		t.Fatal("expected an uncertain write", err)
	}
	result, err := f.manager.Settle(t.Context(), upload.Operation, WriterStoppedAndStorageSettled, nil)
	if err != nil || result.State != CleanupPending || result.Attempts != 0 {
		t.Fatal("settlement ignored cleanup grace", result, err)
	}
	before := cleanupManagerAt(t, f, now.Add(grace-time.Second), grace, testSingle.Registration())
	if _, err := before.ReconcilePending(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
		t.Fatal("settled object deleted before grace", err)
	}
	after := cleanupManagerAt(t, f, now.Add(grace), grace, testSingle.Registration())
	if _, err := after.ReconcilePending(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	assertObjectAbsent(t, f.disk, key)
}
