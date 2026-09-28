package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestDownloadMetadataRejectsInvalidRepresentations(t *testing.T) {
	for _, valid := range []MediaType{"application/octet-stream", "text/plain; charset=utf-8", "application/problem+json"} {
		if err := valid.Validate(); err != nil {
			t.Fatalf("media %q: %v", valid, err)
		}
	}
	for _, invalid := range []MediaType{"", "text", "text/", "image/*", "*/png", "text/plain/extra", "text/plain\r\nSet-Cookie: bad", "text/plain; charset=utf-8; charset=ascii"} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted media %q", invalid)
		}
	}
	for _, valid := range []EntityTag{"", "\"\"", "\"version-1\"", "W/\"version-1\""} {
		if err := valid.Validate(); err != nil {
			t.Fatalf("tag %q: %v", valid, err)
		}
	}
	for _, invalid := range []EntityTag{"*", "version-1", "w/\"x\"", "\"a b\"", "\"a\"b\"", "\"x\r\ny\""} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted tag %q", invalid)
		}
	}
}

func TestDownloadDeclarationDefersOpeningAndRejectsJSON(t *testing.T) {
	calls := 0
	source := func(context.Context) (DownloadContent, error) { calls++; return DownloadContent{}, nil }
	base := DownloadFrom(source)
	adapted := base.WithName("display.pdf").WithDisposition(DispositionInline).WithMediaType("application/pdf").WithEntityTag("\"revision-1\"")
	if err := adapted.Validate(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("declaration opened its source")
	}
	if _, err := json.Marshal(adapted); err == nil {
		t.Fatal("deferred download serialized as JSON")
	}
	for _, invalid := range []Download{{}, DownloadFrom(nil), base.WithDisposition("unknown"), base.WithMediaType("image/*"), base.WithEntityTag("revision-1")} {
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid download accepted")
		}
	}
	if err := base.Validate(); err != nil {
		t.Fatal("derived declaration changed its source", err)
	}
}

func TestLocalDownloadUsesConfinedRegularFilesAndOwnedRoot(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "source.bin"), []byte("file contents"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	download := LocalDownload(root, "source.bin").WithName("display.bin")
	content, err := download.source(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(content.Body)
	closeErr := content.Body.Close()
	if readErr != nil || closeErr != nil || string(data) != "file contents" || content.Name != "source.bin" || content.Modified.IsZero() {
		t.Fatal("local source metadata/content changed")
	}
	// The application root remains owned by the application after file cleanup.
	if _, err := root.Stat("source.bin"); err != nil {
		t.Fatal("source closed its application root", err)
	}
	for _, path := range []string{".", "../source.bin", "/source.bin", "a/../source.bin", "missing.bin"} {
		download := LocalDownload(root, path)
		if err := download.Validate(); err != nil {
			if path == "missing.bin" {
				t.Fatal("declaration checked file existence")
			}
			continue
		}
		if path != "missing.bin" {
			t.Fatalf("invalid path declaration %q passed validation", path)
		}
		content, err := download.source(t.Context())
		if content.Body != nil {
			content.Body.Close()
			t.Fatal("rejected path published an opened body")
		}
		if err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
	if err := root.Mkdir("folder", 0700); err != nil {
		t.Fatal(err)
	}
	if content, err := LocalDownload(root, "folder").source(t.Context()); err == nil || content.Body != nil {
		t.Fatal("directory became downloadable content")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if content, err := LocalDownload(root, "source.bin").source(ctx); !errors.Is(err, context.Canceled) || content.Body != nil {
		t.Fatal("canceled source opened content")
	}
	if err := LocalDownload(nil, "source.bin").Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil root accepted")
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.bin"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink(filepath.Join(outside, "private.bin"), "escape.bin"); err != nil {
		t.Fatal(err)
	}
	content, err = LocalDownload(root, "escape.bin").source(t.Context())
	if content.Body != nil {
		content.Body.Close()
		t.Fatal("outside symlink opened")
	}
	if err == nil {
		t.Fatal("outside symlink accepted")
	}
}
