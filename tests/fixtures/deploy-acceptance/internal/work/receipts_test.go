package work

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testID = "a36ef1ec-5af0-4c21-aaf6-6473284b7db1"

func TestBarrierReceiptRequiresRelease(t *testing.T) {
	root := t.TempDir()
	receipts := NewReceipts(root)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- receipts.Handle(ctx, Payload{ID: testID, Barrier: true}) }()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		if _, err := os.Stat(filepath.Join(root, testID+".started")); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-poll.C:
		}
	}
	if _, err := os.Stat(filepath.Join(root, testID+".completed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("barrier completed before release")
	}
	if err := receipts.Mark(testID + ".release"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, testID+".completed")); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledBarrierNeverCompletes(t *testing.T) {
	root := t.TempDir()
	receipts := NewReceipts(root)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := receipts.Handle(ctx, Payload{ID: testID, Barrier: true}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, testID+".completed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled barrier completed")
	}
}

func TestRejectsNonUUIDAndSymlinkRelease(t *testing.T) {
	root := t.TempDir()
	receipts := NewReceipts(root)
	if err := receipts.Handle(t.Context(), Payload{ID: "../escape"}); err == nil {
		t.Fatal("accepted path traversal")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, testID+".release")); err != nil {
		t.Fatal(err)
	}
	if err := receipts.Handle(t.Context(), Payload{ID: testID, Barrier: true}); err == nil {
		t.Fatal("accepted nonregular release")
	}
}
