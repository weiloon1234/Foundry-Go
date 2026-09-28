package local_test

import (
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
)

type cyclicSourceError struct{ visits atomic.Int32 }

func (*cyclicSourceError) Error() string { panic("private source error formatted") }
func (e *cyclicSourceError) Unwrap() error {
	// Finite escape makes the old implementation fail without hanging the suite.
	if e.visits.Add(1) > 1024 {
		return nil
	}
	return e
}

type rejectedSource struct{ err error }

func (s rejectedSource) Read([]byte) (int, error) { return 0, s.err }

func TestCyclicSourceErrorCleansStagingAndPreservesPublishedObject(t *testing.T) {
	root := t.TempDir()
	disk, _ := openLocal(t, root)
	object := key(t, "preserved")
	original, err := disk.PutBytes(t.Context(), object, []byte("original"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cycle := &cyclicSourceError{}
	_, err = disk.Put(t.Context(), object, io.MultiReader(strings.NewReader("partial"), rejectedSource{cycle}), storage.PutOptions{})
	detail, ok := err.(*storage.Error)
	if !ok || detail.Code() != storage.Unavailable || detail.Outcome() != storage.Unchanged {
		t.Fatal("source failure lost unchanged outcome")
	}
	if cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("source inspection was not bounded", cycle.visits.Load())
	}
	data, info, err := disk.ReadBytes(t.Context(), object, 128, storage.ReadOptions{})
	if err != nil || string(data) != "original" || info.Object.ETag != original.Object.ETag {
		t.Fatal("failed upload changed existing object")
	}
	err = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".foundry-tmp-") {
			t.Error("failed upload retained staging file")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.PutBytes(t.Context(), object, []byte("next"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if disk.Stats().Active != 0 {
		t.Fatal("failed upload retained capacity")
	}
}
