package upload

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPoolConstructionAndCloseNeverClaim(t *testing.T) {
	directory := t.TempDir()
	for range 5 {
		pool, err := NewPool(PoolConfig{Directory: directory, MaxActive: 1, MaxBytes: 128})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Reserve(t.Context(), 1); !errors.Is(err, fault.Closed) {
			t.Fatal("unclaimed pool admitted work", err)
		}
		if err := pool.Close(); err != nil {
			t.Fatal(err)
		}
		if err := pool.Claim(); !errors.Is(err, fault.Closed) {
			t.Fatal("closed pool claimed storage", err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("inspection wrote to the directory", entries, err)
	}
}

func TestPoolReconciliationFailureRefusesReservations(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses filesystem permissions")
	}
	directory := t.TempDir()
	capture := filepath.Join(directory, captureDirectoryPrefix+"retained")
	if err := os.Mkdir(capture, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(capture, "owned"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(capture, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(capture, 0700) })
	pool, err := NewPool(PoolConfig{Directory: directory, MaxActive: 1, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Claim(); !errors.Is(err, fault.Internal) {
		t.Fatal("failed reconciliation admitted serving", err)
	}
	if _, err := pool.Reserve(t.Context(), 1); !errors.Is(err, fault.Closed) {
		t.Fatal("failed reconciliation admitted work", err)
	}
	if err := os.Chmod(capture, 0700); err != nil {
		t.Fatal(err)
	}
	if err := pool.Claim(); err != nil {
		t.Fatal("failed claim retained the lock", err)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("retained capture survived reconciliation", err)
	}
}
