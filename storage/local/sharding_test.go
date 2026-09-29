package local_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestShardLocksSerializeConditionalWritesPerKey(t *testing.T) {
	root := t.TempDir()
	one, _ := openLocal(t, root)
	two, _ := openLocal(t, root)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := range 16 {
		disk := one
		if i%2 == 1 {
			disk = two
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := disk.PutBytes(t.Context(), key(t, "race/same"), []byte("payload"), storage.PutOptions{Condition: storage.IfAbsent()})
			results <- err
		}()
	}
	// Independent keys in other shards publish concurrently with the race.
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := one.PutBytes(t.Context(), key(t, "independent/"+string(rune('a'+i))), []byte("x"), storage.PutOptions{Condition: storage.IfAbsent()}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(results)
	published := 0
	for err := range results {
		if err == nil {
			published++
		} else if !errors.Is(err, storage.PreconditionFailed) {
			t.Fatal(err)
		}
	}
	if published != 1 {
		t.Fatal("conditional create published more than once", published)
	}
}

func TestDelimitedListingReportsDirectoriesAndResumes(t *testing.T) {
	disk, _ := openLocal(t, t.TempDir())
	for _, name := range []string{"docs/a.txt", "docs/b/one.txt", "docs/b/two.txt", "docs/c.txt", "docs/d/deep/x.txt", "other/z"} {
		if _, err := disk.PutBytes(t.Context(), key(t, name), []byte(name), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	prefix, _ := storage.ParsePrefix("docs/")
	options := storage.ListOptions{Prefix: prefix, Limit: 2, Delimited: true}
	var seen []string
	for range 4 {
		page, err := disk.List(t.Context(), options)
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range page.Objects {
			seen = append(seen, object.Key.String())
		}
		for _, directory := range page.Directories {
			seen = append(seen, directory.String())
		}
		if page.Next.IsZero() {
			break
		}
		options.Cursor = page.Next
	}
	counts := make(map[string]int)
	for _, entry := range seen {
		counts[entry]++
	}
	for _, expected := range []string{"docs/a.txt", "docs/b/", "docs/c.txt", "docs/d/"} {
		if counts[expected] != 1 {
			t.Fatal("one-level listing lost or duplicated an entry", seen)
		}
	}
	if len(seen) != 4 {
		t.Fatal("nested objects leaked into a one-level listing", seen)
	}
}

func TestUnreadableRecordsAreSkippedNotFatal(t *testing.T) {
	root := t.TempDir()
	disk, _ := openLocal(t, root)
	if _, err := disk.PutBytes(t.Context(), key(t, "good"), []byte("ok"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("corrupt"))
	name := hex.EncodeToString(sum[:])
	directory := filepath.Join(root, "objects", name[:2])
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), []byte("not a record"), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := disk.List(t.Context(), storage.ListOptions{Limit: 10})
	if err != nil || len(page.Objects) != 1 || page.Skipped != 1 {
		t.Fatal("corrupt record failed or leaked into the page", page.Skipped, err)
	}
}

func TestSameStoreCopyPreservesBytesChecksumAndConditions(t *testing.T) {
	disk, _ := openLocal(t, t.TempDir())
	data := []byte(strings.Repeat("payload", 1000))
	digest := storage.SHA256(sha256.Sum256(data))
	source, err := disk.PutBytes(t.Context(), key(t, "copy/source"), data, storage.PutOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := disk.CopyTo(t.Context(), key(t, "copy/source"), disk, key(t, "copy/target"), storage.CopyOptions{Source: storage.ReadOptions{IfMatch: source.Object.ETag}, Destination: storage.PutOptions{Condition: storage.IfAbsent(), Checksum: value.Set(digest)}})
	if err != nil || copied.Object.Size != int64(len(data)) || copied.Object.ContentType != "text/plain" || copied.Object.ETag == source.Object.ETag {
		t.Fatal("server copy changed metadata", err)
	}
	body, _, err := disk.Open(t.Context(), key(t, "copy/target"), storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(body)
	if cleanup := body.Close(); err != nil || cleanup != nil || string(read) != string(data) {
		t.Fatal("copied bytes differ", err, cleanup)
	}
	if _, err := disk.CopyTo(t.Context(), key(t, "copy/source"), disk, key(t, "copy/target"), storage.CopyOptions{Destination: storage.PutOptions{Condition: storage.IfAbsent()}}); !errors.Is(err, storage.PreconditionFailed) {
		t.Fatal("copy ignored the destination condition", err)
	}
	if _, err := disk.CopyTo(t.Context(), key(t, "copy/source"), disk, key(t, "copy/other"), storage.CopyOptions{Source: storage.ReadOptions{IfMatch: `"00000000000000000000000000000000"`}}); !errors.Is(err, storage.PreconditionFailed) {
		t.Fatal("copy ignored the source pin", err)
	}
	moved, err := disk.MoveTo(t.Context(), key(t, "copy/target"), disk, key(t, "copy/moved"), storage.CopyOptions{})
	if err != nil || moved.SourceOutcome != storage.Applied {
		t.Fatal("move over server copy failed", err)
	}
	if exists, err := disk.Exists(t.Context(), key(t, "copy/target")); err != nil || exists {
		t.Fatal("move kept its source", err)
	}
}

// A server copy never publishes an object beyond either disk's limit.
func TestSameStoreCopyEnforcesObjectLimitsBeforePublishing(t *testing.T) {
	large, backend := openLocal(t, t.TempDir())
	if _, err := large.PutBytes(t.Context(), key(t, "limit/source"), make([]byte, 64), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	config := storage.DefaultConfig()
	config.MaxObjectBytes = 32
	small, err := storage.NewDisk("small", backend, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = small.Close(context.Background()) })
	_, err = large.CopyTo(t.Context(), key(t, "limit/source"), small, key(t, "limit/target"), storage.CopyOptions{})
	var failure *storage.Error
	if !errors.As(err, &failure) || failure.Code() != storage.LimitExceeded || failure.Outcome() != storage.Unchanged {
		t.Fatal("oversized server copy was not rejected before publication", err)
	}
	moved, err := large.MoveTo(t.Context(), key(t, "limit/source"), small, key(t, "limit/target"), storage.CopyOptions{})
	if err == nil || moved.Destination.IsSet() || moved.SourceOutcome != storage.Unchanged {
		t.Fatal("oversized move published or removed an object", err)
	}
	if exists, err := large.Exists(t.Context(), key(t, "limit/target")); err != nil || exists {
		t.Fatal("oversized copy left a destination", err)
	}
}
