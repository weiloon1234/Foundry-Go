package storage

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

// Local supplies a fresh temporary disk backed by the ordinary local adapter.
// It retains conditional writes, path checks and object metadata. Cleanup removes
// only testing.TB's own temporary directory, never a supplied bucket/database.
func Local(t testing.TB, id storage.DiskID) *storage.Disk {
	t.Helper()
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("open test storage: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close test storage backend: %v", err)
		}
	})
	disk, err := storage.NewDisk(id, backend, storage.DefaultConfig())
	if err != nil {
		t.Fatalf("create test disk: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := disk.Close(ctx); err != nil {
			t.Errorf("close test disk: %v", err)
		}
	})
	return disk
}
