// Package storage provides a shared behavioral contract for storage adapters.
// Import it as storagetest to distinguish it from the runtime storage package.
package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Run creates a fresh isolated disk per scenario. The factory owns cleanup and
// must never reset a database/bucket or return a shared production object prefix.
// Capability-specific cases skip only when the adapter explicitly lacks them.
func Run(t *testing.T, create func(*testing.T) *storage.Disk) {
	t.Helper()
	t.Run("metadata-empty-unicode-and-key-identity", func(t *testing.T) {
		disk := create(t)
		for _, name := range []string{"empty", "Mixed/File é.json", "mixed/File é.json", "unicode/é", "unicode/e\u0301", "literal/%2e%2e and ?#.json"} {
			key := key(t, name)
			data := []byte(name)
			if name == "empty" {
				data = nil
			}
			digest := storage.SHA256(sha256.Sum256(data))
			stored, err := disk.PutBytes(t.Context(), key, data, storage.PutOptions{ContentType: "application/json", Checksum: value.Set(digest)})
			if disk.Capabilities().RequiresNFCKeys && name == "unicode/e\u0301" {
				if !errors.Is(err, storage.Unsupported) {
					t.Fatal("noncanonical key was not rejected", err)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if stored.Disk != disk.ID() || stored.Object.Key != key || stored.Object.Size != int64(len(data)) {
				t.Fatal("stored metadata changed identity")
			}
		}
		for _, name := range []string{"Mixed/File é.json", "mixed/File é.json", "unicode/é", "unicode/e\u0301", "literal/%2e%2e and ?#.json", "empty"} {
			object := key(t, name)
			body, info, err := disk.Open(t.Context(), object, storage.ReadOptions{})
			if disk.Capabilities().RequiresNFCKeys && name == "unicode/e\u0301" {
				if !errors.Is(err, storage.Unsupported) || body != nil {
					t.Fatal("noncanonical read addressed another object", err)
				}
				if err := disk.Delete(t.Context(), object, storage.DeleteOptions{}); !errors.Is(err, storage.Unsupported) {
					t.Fatal("noncanonical delete was not rejected", err)
				}
				canonical := key(t, "unicode/é")
				data, _, err := disk.ReadBytes(t.Context(), canonical, 64, storage.ReadOptions{})
				if err != nil || string(data) != "unicode/é" {
					t.Fatal("canonical object was changed by an alias", err)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(body)
			closeErr := body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatal(readErr, closeErr)
			}
			expected := name
			if name == "empty" {
				expected = ""
			}
			if string(data) != expected || info.Object.ContentType != "application/json" || info.Length != int64(len(expected)) {
				t.Fatalf("stored representation changed for %q: content_matches=%t media_matches=%t length=%d expected_length=%d", name, string(data) == expected, info.Object.ContentType == "application/json", info.Length, len(expected))
			}
			stat, err := disk.Stat(t.Context(), object, storage.ReadOptions{})
			if err != nil || stat.Key != object || stat.Size != info.Object.Size || stat.ETag != info.Object.ETag {
				t.Fatal("head and bytes describe different versions", err)
			}
		}
	})
	t.Run("failed-stream-preserves-previous-object", func(t *testing.T) {
		disk := create(t)
		object := key(t, "safe/existing")
		before, err := disk.PutBytes(t.Context(), object, []byte("previous"), storage.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"read-error", "panic", "goexit", "too-short", "too-long", "checksum"} {
			t.Run(mode, func(t *testing.T) {
				var input io.Reader = &brokenReader{mode: mode}
				options := storage.PutOptions{}
				switch mode {
				case "too-short":
					input = strings.NewReader("a")
					options.Size = value.Set[int64](2)
				case "too-long":
					input = strings.NewReader("abc")
					options.Size = value.Set[int64](2)
				case "checksum":
					input = strings.NewReader("abc")
					options.Checksum = value.Set(storage.SHA256{})
				}
				if result, err := disk.Put(t.Context(), object, input, options); err == nil || !result.Object.Key.IsZero() {
					t.Fatal("failed source published an object")
				}
				after, err := disk.Stat(t.Context(), object, storage.ReadOptions{})
				if err != nil || after.ETag != before.Object.ETag {
					t.Fatal("failed write replaced prior object", err)
				}
			})
		}
	})

	t.Run("conditional-create-and-replace", func(t *testing.T) {
		disk := create(t)
		cap := disk.Capabilities()
		if !cap.ConditionalCreate || !cap.ConditionalReplace {
			t.Skip("conditional writes are not supported")
		}
		object := key(t, "conditional/data")
		first, err := disk.PutBytes(t.Context(), object, []byte("first"), storage.PutOptions{Condition: storage.IfAbsent()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := disk.PutBytes(t.Context(), object, []byte("duplicate"), storage.PutOptions{Condition: storage.IfAbsent()}); !errors.Is(err, storage.PreconditionFailed) {
			t.Fatal("duplicate create accepted", err)
		}
		condition, err := storage.IfMatch(first.Object.ETag)
		if err != nil {
			t.Fatal(err)
		}
		second, err := disk.PutBytes(t.Context(), object, []byte("second"), storage.PutOptions{Condition: condition})
		if err != nil {
			t.Fatal(err)
		}
		if second.Object.ETag == first.Object.ETag {
			t.Fatal("different content retained validator")
		}
		if _, err := disk.PutBytes(t.Context(), object, []byte("stale"), storage.PutOptions{Condition: condition}); !errors.Is(err, storage.PreconditionFailed) {
			t.Fatal("stale update accepted", err)
		}
	})
	t.Run("conditional-read-and-range", func(t *testing.T) {
		disk := create(t)
		cap := disk.Capabilities()
		if !cap.ConditionalRead || !cap.Ranges {
			t.Skip("conditional reads/ranges are not supported")
		}
		object := key(t, "read/data")
		first, err := disk.PutBytes(t.Context(), object, []byte("0123456789"), storage.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		second, err := disk.PutBytes(t.Context(), object, []byte("abcdefghij"), storage.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		body, info, err := disk.Open(t.Context(), object, storage.ReadOptions{IfMatch: second.Object.ETag, Range: value.Set(storage.ByteRange{Offset: 7, Length: 99})})
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(body)
		closeErr := body.Close()
		if readErr != nil || closeErr != nil || string(data) != "hij" || info.Object.Size != 10 || info.Offset != 7 || info.Length != 3 {
			t.Fatal("range semantics changed", readErr, closeErr)
		}
		if body, _, err := disk.Open(t.Context(), object, storage.ReadOptions{IfMatch: first.Object.ETag}); !errors.Is(err, storage.PreconditionFailed) || body != nil {
			t.Fatal("stale read accepted", err)
		}
	})
	t.Run("conditional-delete", func(t *testing.T) {
		disk := create(t)
		if !disk.Capabilities().ConditionalDelete {
			t.Skip("conditional deletion is not supported")
		}
		object := key(t, "delete/data")
		first, err := disk.PutBytes(t.Context(), object, []byte("first"), storage.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		second, err := disk.PutBytes(t.Context(), object, []byte("second"), storage.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := disk.Delete(t.Context(), object, storage.DeleteOptions{IfMatch: first.Object.ETag}); !errors.Is(err, storage.PreconditionFailed) {
			t.Fatal("stale deletion accepted", err)
		}
		if err := disk.Delete(t.Context(), object, storage.DeleteOptions{IfMatch: second.Object.ETag}); err != nil {
			t.Fatal(err)
		}
		if err := disk.Delete(t.Context(), object, storage.DeleteOptions{IfMatch: second.Object.ETag}); !errors.Is(err, storage.PreconditionFailed) {
			t.Fatal("missing conditional target accepted", err)
		}
	})
	t.Run("unconditional-delete-is-idempotent", func(t *testing.T) {
		disk := create(t)
		object := key(t, "delete/data")
		if _, err := disk.PutBytes(t.Context(), object, []byte("body"), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := disk.Delete(t.Context(), object, storage.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		if exists, err := disk.Exists(t.Context(), object); err != nil || exists {
			t.Fatal("deleted object exists", err)
		}
	})
	t.Run("bounded-listing-and-cancellation", func(t *testing.T) {
		disk := create(t)
		names := []string{"page/a", "page/b", "page/c", "page/d", "other/a"}
		for _, name := range names {
			if _, err := disk.PutBytes(t.Context(), key(t, name), []byte(name), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		prefix, err := storage.ParsePrefix("page/")
		if err != nil {
			t.Fatal(err)
		}
		options := storage.ListOptions{Prefix: prefix, Limit: 2}
		var seen []string
		for pageNumber := 0; pageNumber < 4; pageNumber++ {
			page, err := disk.List(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Objects) > 2 {
				t.Fatal("list limit ignored")
			}
			for _, info := range page.Objects {
				seen = append(seen, info.Key.String())
			}
			if page.Next.IsZero() {
				break
			}
			options.Cursor = page.Next
		}
		if strings.Join(seen, ",") != "page/a,page/b,page/c,page/d" {
			t.Fatal("pagination lost or duplicated keys")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := disk.Put(ctx, key(t, "canceled"), bytes.NewReader(nil), storage.PutOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled put accepted", err)
		}
		if _, err := disk.List(ctx, options); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled list accepted", err)
		}
	})
}
func key(t *testing.T, text string) storage.ObjectKey {
	t.Helper()
	key, err := storage.ParseKey(text)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

type brokenReader struct{ mode string }

func (r *brokenReader) Read(p []byte) (int, error) {
	switch r.mode {
	case "panic":
		panic("private source text")
	case "goexit":
		runtime.Goexit()
	}
	if len(p) > 0 {
		p[0] = 'x'
		return 1, errors.New("source failed")
	}
	return 0, nil
}
