//go:build unix

package sourcemap

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFIFORejectedWithoutOpening(t *testing.T) {
	c := testConfig(t)
	dir := artifact(t, c, "old", "admin", nil, 0)
	name := filepath.Join(dir, "app.js.map")
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newStore(t, c).Read(context.Background(), "old", "admin/_assets/app.js"); err == nil {
		t.Fatal("accepted FIFO")
	}
}
