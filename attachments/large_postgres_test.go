package attachments

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type countedOriginalBackend struct {
	storage.Backend
	opens atomic.Int64
}

func (b *countedOriginalBackend) Open(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	b.opens.Add(1)
	return b.Backend.Open(ctx, key, options)
}

func TestPostgresHistoricalLargeOriginalRejectsNewVariantsButStillStreamsAndRetires(t *testing.T) {
	original := Define(extensiontest.Members, "historical", largePolicy())
	var backend *countedOriginalBackend
	f := openAttachmentsOn(t, nil, func(inner storage.Backend) storage.Backend {
		backend = &countedOriginalBackend{Backend: inner}
		return backend
	}, original.Registration())
	const size = MaxBufferedBytes + 1
	result, err := original.Add(t.Context(), f.manager, member(t, 1), Upload{Source: io.LimitReader(repeatedByte('x'), size)})
	if err != nil {
		t.Fatal(err)
	}
	file := attachmentOf(t, result)
	changed := Define(extensiontest.Members, "historical", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Variants: []Variant{testThumbnail}})
	manager, err := New(Dependencies{Store: f.Store, Disks: f.registry, Image: f.image}, DefaultConfig(), changed.Registration())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(t.Context())
	before := backend.opens.Load()
	for _, replace := range []bool{false, true} {
		if err := manager.generate(t.Context(), fileID(result.Operation), replace); !errors.Is(err, storage.LimitExceeded) {
			t.Fatal("historical original reached variant processing", err)
		}
	}
	if backend.opens.Load() != before || len(variantRows(t, f, result.Operation)) != 0 {
		t.Fatal("rejected variant read storage or published a variant")
	}
	reader, _, err := changed.Open(t.Context(), manager, member(t, 1), file.ID())
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || n != size {
		t.Fatal("historical streaming read stranded", err, closeErr)
	}
	if _, err := changed.Detach(t.Context(), manager, member(t, 1), file.ID()); err != nil {
		t.Fatal("historical retirement stranded", err)
	}
	if journal(t, f, result.Operation).Size != size {
		t.Fatal("historical tombstone size changed")
	}
}

func TestPostgresLargeURLImportUsesBoundedSpool(t *testing.T) {
	collection := Define(extensiontest.Members, "large-import", largePolicy())
	f := openAttachments(t, collection.Registration()) // Environment gate precedes resources/large work.
	f.manager.config.TempDirectory = t.TempDir()
	const size = MaxBufferedBytes + 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.CopyN(w, repeatedByte('x'), size)
	}))
	defer server.Close()
	client := remoteClient(t, server, true)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result, err := collection.AddFromURL(t.Context(), f.manager, member(t, 1), RemoteSource{Client: client, URL: server.URL + "/file"})
	runtime.ReadMemStats(&after)
	if err != nil || result.Publication != Published {
		t.Fatal("large URL import failed", err)
	}
	if after.TotalAlloc-before.TotalAlloc > 32<<20 {
		t.Fatal("large URL import allocated content-sized memory")
	}
	if attachmentOf(t, result).Info().MediaType != "text/plain" {
		t.Fatal("large URL media hint trusted")
	}
	assertSpoolEmpty(t, f.manager.config.TempDirectory)
}

func TestPostgresOriginalSizeMigrationPreservesRowsAndVariantCeiling(t *testing.T) {
	definitions := Migrations()
	f := openAttachmentsWith(t, definitions[:2], nil, nil)
	result := addText(t, f, testSingle, member(t, 1), "historical")
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		for _, statement := range definitions[2].SQL {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET size=$1 WHERE id=$2`, MaxBufferedBytes+1, result.Operation.String())
		return err
	}); err != nil {
		t.Fatal("forward migration failed", err)
	}
	if row := journal(t, f, result.Operation); row.Size != MaxBufferedBytes+1 || row.OriginalName != "note.txt" {
		t.Fatal("migration lost historical metadata")
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_attachments SET size=-1 WHERE id=$1`, result.Operation.String())
		return err
	}); !errors.Is(err, database.CheckViolation) {
		t.Fatal("negative original size did not fail its constraint", err)
	}
	var variantConstraint string
	if err := f.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		return database.ScanOne(ctx, tx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='foundry_attachment_variants'::regclass AND conname='foundry_attachment_variants_size_check'`, nil, &variantConstraint)
	}); err != nil {
		t.Fatal(err)
	}
	// Actual rejection, not just constraint text inspection.
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO foundry_attachment_variants(id,file_id,name,disk,object_key,content_type,size,digest,etag,object_version,width,height,state,writer_id,attempts,last_failure,created_at,updated_at) SELECT id,id,'test',disk,object_key,content_type,$1,digest,etag,object_version,width,height,'ready',writer_id,attempts,last_failure,created_at,updated_at FROM foundry_attachments WHERE id=$2`, MaxBufferedBytes+1, result.Operation.String())
		return err
	}); !errors.Is(err, database.CheckViolation) {
		t.Fatal("variant ceiling did not reject oversized output", variantConstraint, err)
	}
}

func TestPostgresOriginalSizeMigrationRefusesDrift(t *testing.T) {
	definitions := Migrations()
	f := extensiontest.Open(t, definitions[:2])
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `ALTER TABLE foundry_attachments DROP CONSTRAINT foundry_attachments_size_check, ADD CONSTRAINT foundry_attachments_size_check CHECK (size >= 10)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, definitions[2].SQL[0])
		return err
	}); err == nil {
		t.Fatal("unexpected schema drift accepted")
	}
}

func TestPostgresLargePolicyChecksDiskCeilingWithoutChangingSmallPolicies(t *testing.T) {
	f := openAttachments(t)
	config := storage.DefaultConfig()
	config.MaxObjectBytes = 1 << 20
	disk, err := testDisk.Bind(f.backend, config)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close(t.Context())
	disks, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Dependencies{Store: f.Store, Disks: disks}, DefaultConfig(), testSingle.Registration())
	if err != nil {
		t.Fatal("legacy small-policy startup changed", err)
	}
	if err := manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	large := Define(extensiontest.Members, "large-capability", largePolicy())
	if manager, err := New(Dependencies{Store: f.Store, Disks: disks}, DefaultConfig(), large.Registration()); err == nil {
		_ = manager.Close(t.Context())
		t.Fatal("large policy exceeded disk infrastructure ceiling")
	}
}
