package local_test

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestTransferHelpersBoundBytesAndPreserveFile(t *testing.T) {
	disk, _ := openLocal(t, t.TempDir())
	object := key(t, "file")
	path := filepath.Join(t.TempDir(), "trusted.txt")
	if err := os.WriteFile(path, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.PutFile(t.Context(), object, path, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if data, _, err := disk.ReadBytes(t.Context(), object, 6, storage.ReadOptions{}); !errors.Is(err, storage.LimitExceeded) || data != nil {
		t.Fatal("unbounded read", err)
	}
	data, info, err := disk.ReadBytes(t.Context(), object, 7, storage.ReadOptions{})
	if err != nil || string(data) != "payload" || info.Length != 7 {
		t.Fatal("exact bounded read", err)
	}
	if original, err := os.ReadFile(path); err != nil || string(original) != "payload" {
		t.Fatal("input file modified", err)
	}
	if disk.Stats().Active != 0 {
		t.Fatal("helper leaked reader")
	}
}
func TestCopyMoveAndConcurrentReplacement(t *testing.T) {
	source, _ := openLocal(t, t.TempDir())
	target, targetBackend := openLocal(t, t.TempDir())
	from, to := key(t, "source"), key(t, "target")
	if _, err := source.PutBytes(t.Context(), from, []byte("original"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.CopyTo(t.Context(), from, target, to, storage.CopyOptions{}); err != nil {
		t.Fatal(err)
	}
	if exists, err := source.Exists(t.Context(), from); err != nil || !exists {
		t.Fatal("copy deleted source", err)
	}
	if _, err := source.MoveTo(t.Context(), from, target, to, storage.CopyOptions{Destination: storage.PutOptions{Condition: storage.IfAbsent()}}); !errors.Is(err, storage.PreconditionFailed) {
		t.Fatal("move overwrote conditional target", err)
	}
	wrapper := &afterPut{Backend: targetBackend, after: func() {
		_, err := source.PutBytes(t.Context(), from, []byte("replacement"), storage.PutOptions{})
		if err != nil {
			t.Error(err)
		}
	}}
	intercepted, err := storage.NewDisk("intercepted", wrapper, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer intercepted.Close(context.Background())
	result, err := source.MoveTo(t.Context(), from, intercepted, to, storage.CopyOptions{})
	if !errors.Is(err, storage.PreconditionFailed) || !result.Destination.IsSet() || result.SourceOutcome != storage.Unchanged || result.DestinationOutcome != storage.Applied {
		t.Fatal("move discarded partial state", result, err)
	}
	data, _, err := source.ReadBytes(t.Context(), from, 64, storage.ReadOptions{})
	if err != nil || string(data) != "replacement" {
		t.Fatal("move deleted concurrent source", err)
	}
	data, _, err = target.ReadBytes(t.Context(), to, 64, storage.ReadOptions{})
	if err != nil || string(data) != "original" {
		t.Fatal("move did not preserve copied object", err)
	}
	result, err = source.MoveTo(t.Context(), from, target, key(t, "successful"), storage.CopyOptions{})
	if err != nil || result.SourceOutcome != storage.Applied || !result.Destination.IsSet() {
		t.Fatal("move failed", err)
	}
	if exists, err := source.Exists(t.Context(), from); err != nil || exists {
		t.Fatal("successful move retained source", err)
	}
}

type afterPut struct {
	storage.Backend
	after func()
}

func (b *afterPut) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	info, err := b.Backend.Put(ctx, key, source, options)
	if err == nil {
		b.after()
	}
	return info, err
}
func TestSeekUsesBoundedPinnedRepresentations(t *testing.T) {
	disk, _ := openLocal(t, t.TempDir())
	object := key(t, "seek")
	if _, err := disk.PutBytes(t.Context(), object, []byte("abcdefghij"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	reader, info, err := disk.OpenSeek(t.Context(), object, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if info.Size != 10 || disk.Stats().Active != 0 {
		t.Fatal("seek opened payload eagerly")
	}
	if at, err := reader.Seek(-3, io.SeekEnd); err != nil || at != 7 {
		t.Fatal(at, err)
	}
	tail, err := io.ReadAll(reader)
	if err != nil || string(tail) != "hij" {
		t.Fatal(string(tail), err)
	}
	if _, err := reader.Seek(math.MaxInt64, io.SeekCurrent); !errors.Is(err, storage.Invalid) {
		t.Fatal("seek overflow accepted", err)
	}
	if _, err := disk.Put(t.Context(), object, strings.NewReader("replacement"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); !errors.Is(err, storage.PreconditionFailed) {
		t.Fatal("seek mixed object generations", err)
	}
	if err := reader.Close(); err != nil || disk.Stats().Active != 0 {
		t.Fatal("seek leaked reader", err)
	}
}

func (b *afterPut) Locate(key storage.ObjectKey) (storage.ObjectAddress, error) {
	return b.Backend.(storage.ObjectLocator).Locate(key)
}
func TestMoveSupportsEqualKeysAcrossDistinctStoresAndRejectsAliases(t *testing.T) {
	root := t.TempDir()
	source, _ := openLocal(t, root)
	alias, _ := openLocal(t, root)
	destination, _ := openLocal(t, t.TempDir())
	object := key(t, "same/key")
	if _, err := source.PutBytes(t.Context(), object, []byte("payload"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.MoveTo(t.Context(), object, alias, object, storage.CopyOptions{}); !errors.Is(err, storage.Invalid) {
		t.Fatal("self move through alias accepted", err)
	}
	if _, err := source.MoveTo(t.Context(), object, destination, object, storage.CopyOptions{}); err != nil {
		t.Fatal("distinct-store move rejected", err)
	}
	data, _, err := destination.ReadBytes(t.Context(), object, 32, storage.ReadOptions{})
	if err != nil || string(data) != "payload" {
		t.Fatal("destination disappeared", err)
	}
}
