//go:build darwin || linux

package local

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/filelock"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// A conditional publication or removal holds its key's shard lock until the
// directory sync made it durable; unconditional ones release it before.
func TestConditionalOperationsHoldTheShardLockThroughTheirSync(t *testing.T) {
	backend, err := Open(t.Context(), DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	key, err := storage.ParseKey("durable/object")
	if err != nil {
		t.Fatal(err)
	}
	_, name := address(key)
	var observed []bool
	backend.beforeSync = func(synced storage.ObjectKey) {
		file, err := backend.root.OpenFile(lockDirectory+"/"+name[:2], os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		free, err := filelock.Try(file)
		if err != nil {
			t.Error(err)
		}
		observed = append(observed, !free)
	}
	put := func(condition storage.WriteCondition) storage.ObjectInfo {
		t.Helper()
		info, err := backend.Put(context.Background(), key, strings.NewReader("payload"), storage.PutOptions{Size: value.Set[int64](7), Condition: condition})
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	info := put(storage.IfAbsent())
	replace, err := storage.IfMatch(info.ETag)
	if err != nil {
		t.Fatal(err)
	}
	info = put(replace)
	if err := backend.Delete(t.Context(), key, storage.DeleteOptions{IfMatch: info.ETag}); err != nil {
		t.Fatal(err)
	}
	put(storage.WriteCondition{})
	if err := backend.Delete(t.Context(), key, storage.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	want := []bool{true, true, true, false, false}
	if len(observed) != len(want) {
		t.Fatal("sync observations", observed)
	}
	for i := range want {
		if observed[i] != want[i] {
			t.Fatal("shard lock state before sync", i, observed)
		}
	}
}
