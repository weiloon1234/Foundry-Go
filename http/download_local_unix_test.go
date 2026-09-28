//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package http

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLocalDownloadRejectsFIFOWithoutWaitingForAWriter(t *testing.T) {
	directory := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(directory, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	content, err := LocalDownload(root, "pipe").source(t.Context())
	if content.Body != nil {
		content.Body.Close()
		t.Fatal("FIFO opened for download")
	}
	if err == nil {
		t.Fatal("FIFO accepted as a regular file")
	}
}
