package attachments

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/imaging"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/inline"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

var (
	testThumbnail = DefineVariant("thumbnail", imaging.NewPlan().Fit(4, 4, false).Format(imaging.WebP))
	testPreview   = DefineVariant("preview", imaging.NewPlan().Fit(8, 8, false).Format(imaging.JPEG))
	testPhotos    = Define(extensiontest.Members, "photos", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Variants: []Variant{testThumbnail, testPreview}})
)

func pngUpload(t *testing.T, width, height int) Upload {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 13), G: uint8(y * 29), B: 80, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return Upload{Source: bytes.NewReader(encoded.Bytes()), OriginalName: "photo.png"}
}
func variantRows(t *testing.T, f attachmentFixture, id OperationID) []store.Variant {
	t.Helper()
	var rows []store.Variant
	err := f.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		v := store.VariantFields()
		var err error
		rows, err = store.QueryFoundryAttachmentVariants().Where(v.FileID.Eq(fileID(id))).OrderBy(v.Name.Asc(), v.CreatedAt.Asc()).All(ctx, tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
func assertVariantsCleaned(t *testing.T, f attachmentFixture, disk *storage.Disk, id OperationID) {
	t.Helper()
	rows := variantRows(t, f, id)
	if len(rows) == 0 {
		t.Fatal("original had no variant journal")
	}
	for _, row := range rows {
		key, err := storage.ParseKey(row.ObjectKey)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != string(VariantCleaned) {
			t.Fatal("variant did not follow its original's lifecycle", row.Name, row.State, row.LastFailure)
		}
		if _, err := disk.Stat(t.Context(), key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
			t.Fatal("cleaned variant object retained", err)
		}
	}
}

func TestPostgresAttachmentVariantsGenerateAndFollowTheOriginal(t *testing.T) {
	f := openAttachments(t)
	m, disk, links := linkedManager(t, f, testPhotos.Registration())
	owner := member(t, 1)
	first, err := testPhotos.Add(t.Context(), m, owner, pngUpload(t, 16, 8))
	if err != nil || first.Publication != Published || first.PendingVariants {
		t.Fatal("synchronous variant generation failed", err)
	}
	original := attachmentOf(t, first)
	thumbnail, ok := original.Variant(testThumbnail)
	if !ok || thumbnail.MediaType != "image/webp" || thumbnail.Width != 4 || thumbnail.Height != 2 || len(original.Variants()) != 2 {
		t.Fatal("published attachment lacks its variants", original.Variants())
	}
	if original.Info().MediaType != "image/png" || original.Info().OriginalName != "photo.png" {
		t.Fatal("variant generation changed the original")
	}
	files, err := testPhotos.List(t.Context(), m, owner)
	if err != nil || len(files) != 1 || len(files[0].Variants()) != 2 {
		t.Fatal("loaded attachment lacks its variants", err)
	}
	links.mu.Lock()
	before := links.stats
	links.mu.Unlock()
	address, err := testPhotos.VariantPublicURLOf(t.Context(), m, files[0], testThumbnail)
	if err != nil || !strings.HasPrefix(address, "https://cdn.example/foundry-attachment-variants/") {
		t.Fatal("variant public URL", err, address)
	}
	if _, err := testPhotos.VariantTemporaryURLOf(t.Context(), m, files[0], testPreview, storage.LinkOptions{ExpiresIn: time.Minute}); err != nil {
		t.Fatal(err)
	}
	links.mu.Lock()
	stats := links.stats
	links.mu.Unlock()
	if stats != before {
		t.Fatal("variant links performed storage I/O")
	}
	if _, err := testPhotos.VariantPublicURLOf(t.Context(), m, files[0], DefineVariant("undeclared", imaging.NewPlan())); !errors.Is(err, fault.Invalid) {
		t.Fatal("undeclared variant linked", err)
	}
	for _, stored := range files[0].variants {
		data, _, err := disk.ReadBytes(t.Context(), stored.key, 1<<20, storage.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		info, err := imaging.Inspect(data, f.image.Limits())
		if err != nil || info.Width != stored.info.Width || info.Height != stored.info.Height || storage.MediaType(info.Format.MediaType()) != stored.info.MediaType {
			t.Fatal("stored variant is not the derived image", err)
		}
	}

	// Replacing the original removes its variants with it.
	second, err := testPhotos.Replace(t.Context(), m, owner, pngUpload(t, 8, 8))
	if err != nil || len(second.PendingCleanup) != 0 || len(attachmentOf(t, second).Variants()) != 2 {
		t.Fatal("replacement variants", err)
	}
	assertVariantsCleaned(t, f, disk, first.Operation)

	// A retained original is handed over without its derived variants.
	retained, err := testPhotos.DetachKeepFile(t.Context(), m, owner, attachmentOf(t, second).ID())
	if err != nil || !retained.File.IsSet() {
		t.Fatal("retain", err)
	}
	assertVariantsCleaned(t, f, disk, second.Operation)
	kept, _ := retained.File.Get()
	if _, err := disk.Stat(t.Context(), kept.Key, storage.ReadOptions{}); err != nil {
		t.Fatal("retained original was removed", err)
	}

	// Owner deletion cleans variants before the original.
	other := member(t, 2)
	third, err := testPhotos.Add(t.Context(), m, other, pngUpload(t, 6, 6))
	if err != nil {
		t.Fatal(err)
	}
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=2`); err != nil {
			return err
		}
		return Cleanup(ctx, tx, m, extensiontest.Members, other, lifecycle.Delete, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	assertVariantsCleaned(t, f, disk, third.Operation)
	if journal(t, f, third.Operation).State != string(Cleaned) {
		t.Fatal("owner deletion left the original")
	}
}

func TestPostgresAttachmentVariantFailuresKeepCleanupRetryable(t *testing.T) {
	f := openAttachments(t, testPhotos.Registration())
	owner := member(t, 1)
	variantKey := func(key storage.ObjectKey) bool { return strings.HasPrefix(key.String(), variantPrefix) }
	// A lost variant acknowledgement never unpublishes the original.
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		if variantKey(key) {
			return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
		}
		return f.backend.Backend.Put(ctx, key, source, options)
	})
	result, err := testPhotos.Add(t.Context(), f.manager, owner, pngUpload(t, 8, 8))
	if err == nil || result.Publication != Published || !result.PendingVariants || len(attachmentOf(t, result).Variants()) != 0 {
		t.Fatal("variant failure changed publication", err)
	}
	f.backend.setPut(nil)
	for _, row := range variantRows(t, f, result.Operation) {
		if row.State != string(VariantUncertain) {
			t.Fatal("unknown variant write was not uncertain", row.State)
		}
	}
	// Explicit generation creates the missing variants; retries are idempotent.
	if err := f.manager.GenerateVariants(t.Context(), result.Operation); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.GenerateVariants(t.Context(), result.Operation); err != nil {
		t.Fatal(err)
	}
	ready := 0
	for _, row := range variantRows(t, f, result.Operation) {
		if row.State == string(VariantReady) {
			ready++
		}
	}
	if ready != 2 {
		t.Fatal("idempotent generation duplicated or missed variants", ready)
	}
	// Aged unresolved variant intents are settled by the ordinary sweep.
	err = f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_attachment_variants SET updated_at = updated_at - interval '2 hours' WHERE state = $1`, string(VariantUncertain))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.ReconcilePending(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	for _, row := range variantRows(t, f, result.Operation) {
		if row.State != string(VariantReady) && row.State != string(VariantCleaned) {
			t.Fatal("aged variant intent was not settled", row.State)
		}
	}
	// A failed variant deletion keeps the original pending, then retries.
	f.backend.setDelete(func(ctx context.Context, key storage.ObjectKey, options storage.DeleteOptions) error {
		if variantKey(key) {
			return storage.Failure(storage.Unavailable, storage.DeleteOperation, storage.Unchanged, nil)
		}
		return f.backend.Backend.Delete(ctx, key, options)
	})
	change, err := testPhotos.Detach(t.Context(), f.manager, owner, attachmentOf(t, result).ID())
	if err == nil || len(change.PendingCleanup) != 1 || journal(t, f, result.Operation).State != string(CleanupPending) {
		t.Fatal("variant deletion failure did not keep the original pending", err)
	}
	original := journal(t, f, result.Operation)
	key, _ := storage.ParseKey(original.ObjectKey)
	if _, err := f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
		t.Fatal("original deleted before its variants", err)
	}
	f.backend.setDelete(nil)
	if state, err := f.manager.Reconcile(t.Context(), result.Operation); err != nil || state.State != Cleaned {
		t.Fatal("cleanup retry", err)
	}
	assertVariantsCleaned(t, f, f.disk, result.Operation)
}

func TestPostgresAttachmentVariantsQueueThroughTypedJob(t *testing.T) {
	f := openAttachmentsWith(t, append(Migrations(), outbox.Migrations()...), nil, nil)
	m, _, _ := linkedManager(t, f, testPhotos.Registration())
	policy := jobs.DefaultPolicy("variants")
	policy.Backoff, policy.Jitter = []time.Duration{time.Millisecond}, 0
	job := DefineVariantJob("attachments.variants", policy)
	declaration, err := job.Declare(m)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		t.Fatal(err)
	}
	config := memory.DefaultConfig()
	config.Clock = clock.System{}
	backend, err := inline.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(testkit.Namespace(t)))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := jobs.PrepareOutbox("attachments", dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := job.ToOutbox(producer)
	if err != nil {
		t.Fatal(err)
	}
	photos := testPhotos.WithVariantQueue(queue)
	owner := member(t, 1)
	result, err := photos.Add(t.Context(), m, owner, pngUpload(t, 12, 12))
	if err != nil || !result.PendingVariants || len(attachmentOf(t, result).Variants()) != 0 {
		t.Fatal("queued variants were generated synchronously", err)
	}
	var queued int
	err = f.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		return database.ScanOne(ctx, tx, `SELECT count(*) FROM foundry_outbox`, nil, &queued)
	})
	if err != nil || queued != 1 {
		t.Fatal("publication transaction did not enqueue one variant job", err, queued)
	}
	// Deliver the job as a worker would; the handler is idempotent.
	for range 2 {
		if _, err := job.definition.Dispatch(t.Context(), dispatcher, VariantRequest{Operation: result.Operation}, jobs.Options[VariantRequest]{}); err != nil {
			t.Fatal(err)
		}
	}
	files, err := photos.List(t.Context(), m, owner)
	if err != nil || len(files) != 1 || len(files[0].Variants()) != 2 {
		t.Fatal("variant job did not generate variants", err)
	}
	if _, err := photos.VariantPublicURLOf(t.Context(), m, files[0], testPreview); err != nil {
		t.Fatal(err)
	}
	ready := 0
	for _, row := range variantRows(t, f, result.Operation) {
		if row.State == string(VariantReady) {
			ready++
		}
	}
	if ready != 2 {
		t.Fatal("repeated job duplicated variants", ready)
	}
}

func TestPostgresAttachmentVariantRegenerationPagesAndReplaces(t *testing.T) {
	f := openAttachments(t, testPhotos.Registration())
	var operations []OperationID
	for id := int64(1); id <= 3; id++ {
		result, err := testPhotos.Add(t.Context(), f.manager, member(t, id), pngUpload(t, 10, 10))
		if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, result.Operation)
	}
	before := variantRows(t, f, operations[0])
	var regenerated []OperationID
	options := RegenerationOptions{Limit: 2}
	pages := 0
	for {
		page, err := testPhotos.RegenerateVariants(t.Context(), f.manager, options)
		if err != nil || len(page.Failed) != 0 {
			t.Fatal("regeneration failed", err, page.Failed)
		}
		pages++
		regenerated = append(regenerated, page.Regenerated...)
		if page.Next.IsZero() {
			break
		}
		options.Cursor = page.Next
	}
	if pages != 2 || len(regenerated) != 3 {
		t.Fatal("regeneration did not page through every ready attachment", pages, len(regenerated))
	}
	after := variantRows(t, f, operations[0])
	if len(after) != 4 {
		t.Fatal("regeneration did not replace both variants", len(after))
	}
	for _, row := range before {
		index := slices.IndexFunc(after, func(candidate store.Variant) bool { return candidate.ID == row.ID })
		if index < 0 || after[index].State != string(VariantCleaned) {
			t.Fatal("replaced variant was not cleaned")
		}
	}
	// Missing-only regeneration leaves complete attachments unchanged.
	page, err := testPhotos.RegenerateVariants(t.Context(), f.manager, RegenerationOptions{Missing: true, Limit: 10})
	if err != nil || len(page.Regenerated) != 3 || len(variantRows(t, f, operations[0])) != 4 {
		t.Fatal("missing-only regeneration rewrote existing variants", err)
	}
	for _, options := range []RegenerationOptions{{Limit: 0}, {Limit: MaxRegenerationBatch + 1}} {
		if _, err := testPhotos.RegenerateVariants(t.Context(), f.manager, options); !errors.Is(err, fault.Invalid) {
			t.Fatal("unbounded regeneration accepted", err)
		}
	}
	if _, err := f.manager.RegenerateVariants(t.Context(), extensiontest.Members.Name(), testSingle.Name(), RegenerationOptions{Limit: 1}); !errors.Is(err, fault.Invalid) {
		t.Fatal("collection without variants regenerated", err)
	}
}

func TestVariantDeclarationsRequireImagesAndUniqueNames(t *testing.T) {
	thumbnail := DefineVariant("thumbnail", imaging.NewPlan().Fit(4, 4, false))
	for _, policy := range []Policy{
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"text/plain"}, Variants: []Variant{thumbnail}},
		{Disk: testDisk, Cardinality: Single, AnyMedia: true, Variants: []Variant{thumbnail}},
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Variants: []Variant{thumbnail, thumbnail}},
		{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Variants: []Variant{DefineVariant("Bad Name", imaging.NewPlan())}},
	} {
		if policy.normalized().Validate() == nil {
			t.Fatal("invalid variant policy accepted")
		}
	}
	if err := (Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png", "image/jpeg", "image/avif", "image/svg+xml", "image/heic"}, Variants: []Variant{thumbnail}}).normalized().Validate(); err != nil {
		t.Fatal(err)
	}
}
