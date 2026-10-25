package oracle

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrameworkCaptureIntegrity(t *testing.T) {
	for _, capture := range []struct {
		dir   string
		count int
	}{{"testdata/stage8d", 41}, {"testdata/stage8d-matching", 12}} {
		b, err := Load(capture.dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Records) != capture.count {
			t.Fatal("framework capture coverage changed", len(b.Records))
		}
		// Sources are historical inputs: compare archived capture files, not the
		// migrated tests. All other hashes belong to the recorded immutable commit.
		err = filepath.WalkDir(filepath.Join(capture.dir, "sources"), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(filepath.Join(capture.dir, "sources"), path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			original := strings.TrimSuffix(filepath.ToSlash(rel), ".txt")
			if b.Sources[original] != Digest(data) {
				t.Errorf("archived capture source changed: %s", original)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
