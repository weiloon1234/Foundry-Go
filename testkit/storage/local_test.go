package storage_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	storagetest "github.com/weiloon1234/Foundry-Go/testkit/storage"
)

func TestLocalHelperOwnsAnIsolatedProductionDisk(t *testing.T) {
	key, err := storage.ParseKey("test/record")
	if err != nil {
		t.Fatal(err)
	}
	var retained *storage.Disk
	t.Run("owner", func(t *testing.T) {
		retained = storagetest.Local(t, "same-id")
		other := storagetest.Local(t, "same-id")
		if _, err := retained.PutBytes(t.Context(), key, []byte("owned"), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := other.Stat(t.Context(), key, storage.ReadOptions{}); err == nil {
			t.Fatal("temporary disks share data")
		}
		data, _, err := retained.ReadBytes(t.Context(), key, 20, storage.ReadOptions{})
		if err != nil || string(data) != "owned" {
			t.Fatal(err)
		}
	})
	if _, err := retained.Stat(t.Context(), key, storage.ReadOptions{}); err == nil {
		t.Fatal("owned disk remained open")
	}
}
