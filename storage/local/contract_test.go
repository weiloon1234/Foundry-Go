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
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	storagetest "github.com/weiloon1234/Foundry-Go/testkit/storage"
)

func openLocal(t *testing.T, path string) (*storage.Disk, *local.Backend) {
	t.Helper()
	backend, err := local.Open(t.Context(), local.DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := storage.NewDisk("local", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := disk.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	return disk, backend
}
func key(t *testing.T, name string) storage.ObjectKey {
	t.Helper()
	k, err := storage.ParseKey(name)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func TestLocalSharedStorageContract(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) *storage.Disk { disk, _ := openLocal(t, t.TempDir()); return disk })
}
func TestLocalReopenAndIndependentWritersPreserveConditionalIdentity(t *testing.T) {
	root := t.TempDir()
	one, _ := openLocal(t, root)
	two, _ := openLocal(t, root)
	object := key(t, "shared/data")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, disk := range []*storage.Disk{one, two} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := disk.PutBytes(t.Context(), object, []byte("same payload"), storage.PutOptions{Condition: storage.IfAbsent()})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, storage.PreconditionFailed) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("independent local writers lost conditional exclusion")
	}
	third, _ := openLocal(t, root)
	body, _, err := third.Open(t.Context(), object, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if closeErr := body.Close(); err != nil || closeErr != nil || string(data) != "same payload" {
		t.Fatal("reopened object changed", err, closeErr)
	}
}
func TestLocalRejectsUnmanagedRootAndSymlinkEscape(t *testing.T) {
	unrelated := t.TempDir()
	existing := filepath.Join(unrelated, "existing")
	if err := os.WriteFile(existing, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if backend, err := local.Open(t.Context(), local.DefaultConfig(unrelated)); err == nil || backend != nil {
		t.Fatal("unmanaged directory accepted")
	}
	entries, err := os.ReadDir(unrelated)
	if err != nil || len(entries) != 1 {
		t.Fatal("unmanaged root modified", err)
	}
	root := t.TempDir()
	disk, _ := openLocal(t, root)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "objects")); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.PutBytes(t.Context(), key(t, "escape"), []byte("private"), storage.PutOptions{}); err == nil {
		t.Fatal("symlink escape accepted")
	}
	entries, err = os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside root was modified", err)
	}
}
func TestLocalDetectsPayloadCorruptionAndRejectsForeignCursors(t *testing.T) {
	root := t.TempDir()
	disk, _ := openLocal(t, root)
	object := key(t, "integrity")
	if _, err := disk.PutBytes(t.Context(), object, []byte("payload"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(object.String()))
	name := hex.EncodeToString(digest[:])
	file, err := os.OpenFile(filepath.Join(root, "objects", name[:2], name), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteAt([]byte("X"), info.Size()-1); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	body, _, err := disk.Open(t.Context(), object, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(body)
	_ = body.Close()
	if !errors.Is(err, storage.IntegrityFailed) {
		t.Fatal("corrupt payload accepted", err)
	}
	for _, name := range []string{"page/a", "page/b"} {
		if _, err := disk.PutBytes(t.Context(), key(t, name), []byte("value"), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	prefix, _ := storage.ParsePrefix("page/")
	page, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 1})
	if err != nil || page.Next.IsZero() {
		t.Fatal(err)
	}
	other, _ := openLocal(t, t.TempDir())
	if _, err := other.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 1, Cursor: page.Next}); !errors.Is(err, storage.Invalid) {
		t.Fatal("foreign cursor accepted", err)
	}
	if _, err := disk.List(t.Context(), storage.ListOptions{Limit: 1, Cursor: page.Next}); !errors.Is(err, storage.Invalid) {
		t.Fatal("cursor changed prefix", err)
	}
}
func TestLocalCleanupSkipsLiveAndRemovesInterruptedStaging(t *testing.T) {
	root := t.TempDir()
	disk, backend := openLocal(t, root)
	reader := &pausedSource{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	var resume sync.Once
	release := func() { resume.Do(func() { close(reader.release) }) }
	defer release()
	go func() { _, err := disk.Put(t.Context(), key(t, "slow"), reader, storage.PutOptions{}); done <- err }()
	<-reader.started
	if count, err := backend.Prune(t.Context(), time.Now(), local.MaxPrune); err != nil || count != 0 {
		t.Fatal("cleanup removed a live upload", count, err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "objects", "ff")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(directory, ".foundry-tmp-"+strings.Repeat("a", 32))
	if err := os.WriteFile(orphan, []byte("interrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}
	if count, err := backend.Prune(t.Context(), time.Now(), 1); err != nil || count != 1 {
		t.Fatal("cleanup missed abandoned staging", count, err)
	}
	if exists, err := disk.Exists(t.Context(), key(t, "slow")); err != nil || !exists {
		t.Fatal("cleanup removed a publication", err)
	}
}

type pausedSource struct {
	started, release chan struct{}
	once             sync.Once
}

func (r *pausedSource) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return 0, io.EOF
}
