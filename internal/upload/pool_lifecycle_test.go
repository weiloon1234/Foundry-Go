package upload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
)

func TestPoolOwnerFormatControlsReconciliation(t *testing.T) {
	for _, test := range []struct {
		name    string
		exists  bool
		format  string
		locked  bool
		failure error
	}{
		{name: "no marker"},
		{name: "current stale marker", exists: true, format: ownerFormat},
		{name: "current live marker", exists: true, format: ownerFormat, locked: true, failure: fault.Conflict},
		{name: "legacy empty marker", exists: true, failure: fault.Conflict},
		{name: "unknown version", exists: true, format: "foundry-upload-owner 99\n", failure: fault.Invalid},
		{name: "partial marker", exists: true, format: "foundry-upload-owner", failure: fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			marker := filepath.Join(directory, ownerMarker)
			if test.exists {
				if err := os.WriteFile(marker, []byte(test.format), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if test.locked {
				owner, err := os.OpenFile(marker, os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				if locked, err := filelock.Try(owner); err != nil || !locked {
					t.Fatal("could not hold live owner lock", err)
				}
			}
			capture := filepath.Join(directory, captureDirectoryPrefix+"retained")
			if err := os.Mkdir(capture, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(capture, "owned")
			if err := os.WriteFile(file, []byte("retain until safe"), 0600); err != nil {
				t.Fatal(err)
			}
			pool, err := NewPool(PoolConfig{Directory: directory, MaxActive: 1, MaxBytes: 128})
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			err = pool.Claim()
			if test.failure != nil {
				if !errors.Is(err, test.failure) {
					t.Fatal("unsafe marker did not refuse claim", err)
				}
				if contents, readErr := os.ReadFile(marker); readErr != nil || string(contents) != test.format {
					t.Fatal("refused claim changed marker", readErr)
				}
				if contents, readErr := os.ReadFile(file); readErr != nil || string(contents) != "retain until safe" {
					t.Fatal("refused claim reconciled captures", readErr)
				}
				if _, err := pool.Reserve(t.Context(), 1); !errors.Is(err, fault.Closed) {
					t.Fatal("refused claim admitted work", err)
				}
				if test.format != "" {
					return
				}
				if !strings.Contains(err.Error(), "a v0.30.0 ownership marker was found; stop every v0.30.0 server using this directory, then remove the marker") {
					t.Fatal("legacy marker refusal omitted migration instructions", err)
				}
				// This test owns the legacy marker and has no legacy server.
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				err = pool.Claim()
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(capture); !os.IsNotExist(err) {
				t.Fatal("safe claim did not reconcile capture", err)
			}
			if contents, err := os.ReadFile(marker); err != nil || string(contents) != ownerFormat {
				t.Fatal("claim did not retain current owner format", err)
			}
			reservation, err := pool.Reserve(t.Context(), 128)
			if err != nil {
				t.Fatal(err)
			}
			reservation.Release()
		})
	}
}

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
