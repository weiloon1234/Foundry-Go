package attachments

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

// linkingBackend adds link signing to real local storage and records every
// storage request made while links are derived.
type linkingBackend struct {
	storage.Backend
	mu     sync.Mutex
	stats  int
	signed []storage.LinkOptions
}

func (b *linkingBackend) Stat(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (storage.ObjectInfo, error) {
	b.mu.Lock()
	b.stats++
	b.mu.Unlock()
	return b.Backend.Stat(ctx, key, options)
}
func (b *linkingBackend) PublicURL(_ context.Context, key storage.ObjectKey) (string, error) {
	return "https://cdn.example/" + key.String(), nil
}
func (b *linkingBackend) TemporaryURL(_ context.Context, key storage.ObjectKey, options storage.LinkOptions) (storage.TemporaryURL, error) {
	b.mu.Lock()
	b.signed = append(b.signed, options)
	b.mu.Unlock()
	return storage.NewTemporaryURL("https://signed.example/"+key.String()+"?signature=fixture", time.Now().Add(options.ExpiresIn))
}

// linkedManager binds the test disk to link-signing local storage with public
// visibility, sharing the fixture's database and image engine.
func linkedManager(t *testing.T, f attachmentFixture, registrations ...Registration) (*Manager, *storage.Disk, *linkingBackend) {
	t.Helper()
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	links := &linkingBackend{Backend: backend}
	config := storage.DefaultConfig()
	config.Visibility = storage.Public
	disk, err := testDisk.Bind(links, config)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Dependencies{Store: f.Store, Disks: registry, Image: f.image}, DefaultConfig(), registrations...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := disk.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	return m, disk, links
}

func TestPostgresAttachmentLinksDeriveFromLoadedRowsWithoutIO(t *testing.T) {
	f := openAttachments(t)
	m, disk, links := linkedManager(t, f, testSingle.Registration(), testMultiple.Registration())
	owner := member(t, 1)
	for _, text := range []string{"first document", "second document"} {
		if _, err := testMultiple.Add(t.Context(), m, owner, uploadText(text)); err != nil {
			t.Fatal(err)
		}
	}
	files, err := testMultiple.List(t.Context(), m, owner)
	if err != nil || len(files) != 2 {
		t.Fatal(err)
	}
	links.mu.Lock()
	before := links.stats
	links.mu.Unlock()
	urls, err := testMultiple.PublicURLsOf(t.Context(), m, files)
	if err != nil || len(urls) != 2 || urls[0] != "https://cdn.example/"+files[0].key.String() || urls[1] != "https://cdn.example/"+files[1].key.String() {
		t.Fatal("public URLs were not derived in order", err, urls)
	}
	link, err := testMultiple.TemporaryURLOf(t.Context(), m, files[0], storage.LinkOptions{ExpiresIn: time.Minute, ResponseContentDisposition: `attachment; filename="note.txt"`})
	if err != nil || !strings.HasPrefix(link.URL(), "https://signed.example/") {
		t.Fatal("temporary link was not signed", err)
	}
	links.mu.Lock()
	stats, signed := links.stats, append([]storage.LinkOptions(nil), links.signed...)
	links.mu.Unlock()
	if stats != before || len(signed) != 1 || signed[0].Version != files[0].version || signed[0].ResponseContentDisposition != `attachment; filename="note.txt"` {
		t.Fatal("links from loaded rows performed storage I/O or lost their pin", stats-before, signed)
	}
	// A loaded attachment only yields links through its own collection.
	if _, err := testSingle.PublicURLOf(t.Context(), m, files[0]); err == nil {
		t.Fatal("attachment linked through another collection")
	}
	if _, err := testMultiple.TemporaryURLOf(t.Context(), m, files[0], storage.LinkOptions{ExpiresIn: time.Minute, Version: "other"}); err == nil {
		t.Fatal("caller replaced the stored version pin")
	}
	// Streaming reads verify the pinned digest and hold one disk stream slot.
	body, file, err := testMultiple.Open(t.Context(), m, owner, files[1].ID())
	if err != nil || file.ID() != files[1].ID() || disk.Stats().Streams != 1 {
		t.Fatal("attachment stream did not open", err)
	}
	data, err := io.ReadAll(body)
	if closeErr := body.Close(); err != nil || closeErr != nil || string(data) != "second document" {
		t.Fatal("attachment stream changed bytes", err, closeErr)
	}
	if disk.Stats().Streams != 0 {
		t.Fatal("closed attachment stream retained capacity")
	}
}

var (
	testDrawings       = Define(extensiontest.Members, "drawings", Policy{Disk: testDisk, Cardinality: Multiple, MaxFiles: 3, Accepted: []storage.MediaType{"image/svg+xml"}})
	testTrustedDrawing = Define(extensiontest.Members, "trusted-drawings", Policy{Disk: testDisk, Cardinality: Multiple, MaxFiles: 3, Accepted: []storage.MediaType{"image/svg+xml"}, InlineActiveContent: true})
)

// SVG, HTML and XML run scripts in the serving origin when rendered inline.
func TestPostgresAttachmentScriptCapableFilesAreNeverLinkedInlineByDefault(t *testing.T) {
	f := openAttachments(t)
	m, _, links := linkedManager(t, f, testDrawings.Registration(), testTrustedDrawing.Registration())
	owner := member(t, 1)
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`
	result, err := testDrawings.Add(t.Context(), m, owner, Upload{Source: strings.NewReader(svg), OriginalName: "drawing.svg"})
	if err != nil {
		t.Fatal(err)
	}
	file := attachmentOf(t, result)
	if file.Info().MediaType != "image/svg+xml" {
		t.Fatal("SVG was not detected", file.Info().MediaType)
	}
	if _, err := testDrawings.PublicURLOf(t.Context(), m, file); !errors.Is(err, ActiveContentRefused) || !errors.Is(err, fault.Invalid) {
		t.Fatal("script-capable file got an inline public URL", err)
	}
	if _, err := testDrawings.PublicURL(t.Context(), m, owner, file.ID()); !errors.Is(err, ActiveContentRefused) {
		t.Fatal("script-capable file got an inline public URL", err)
	}
	if _, err := testDrawings.TemporaryURLOf(t.Context(), m, file, storage.LinkOptions{ExpiresIn: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if _, err := testDrawings.TemporaryURLOf(t.Context(), m, file, storage.LinkOptions{ExpiresIn: time.Minute, ResponseContentDisposition: `inline; filename="drawing.svg"`}); !errors.Is(err, ActiveContentRefused) {
		t.Fatal("script-capable file was signed for inline rendering", err)
	}
	if _, err := testDrawings.TemporaryURL(t.Context(), m, owner, file.ID(), time.Minute); err != nil {
		t.Fatal(err)
	}
	links.mu.Lock()
	signed := append([]storage.LinkOptions(nil), links.signed...)
	links.mu.Unlock()
	if len(signed) != 2 || signed[0].ResponseContentDisposition != "attachment" || signed[1].ResponseContentDisposition != "attachment" {
		t.Fatal("signed links to script-capable files do not force a download", signed)
	}
	trusted, err := testTrustedDrawing.Add(t.Context(), m, owner, Upload{Source: strings.NewReader(svg), OriginalName: "drawing.svg"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testTrustedDrawing.PublicURLOf(t.Context(), m, attachmentOf(t, trusted)); err != nil {
		t.Fatal("explicit InlineActiveContent policy was not honored", err)
	}
	for _, media := range []storage.MediaType{"image/svg+xml", "text/html", "application/xhtml+xml", "text/xml", "application/rss+xml", "text/javascript"} {
		if !activeContent(media) {
			t.Fatal("script-capable media not recognized", media)
		}
	}
	for _, media := range []storage.MediaType{"image/png", "application/pdf", "text/plain", "application/json"} {
		if activeContent(media) {
			t.Fatal("passive media treated as script-capable", media)
		}
	}
}
