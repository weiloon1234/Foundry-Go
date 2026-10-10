package logging

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestFileControlsDrainWritesAndKeepActiveDescriptor(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "async"}[async], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "app.jsonl")
			sink, err := PrepareSink(SinkConfig{Driver: File, Path: path, Async: AsyncConfig{Enabled: async}})
			if err != nil {
				t.Fatal(err)
			}
			if err := sink.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer sink.Close()
			for range 20 {
				if _, err := sink.Write([]byte("before\n")); err != nil {
					t.Fatal(err)
				}
			}
			files, err := sink.Files(ctx)
			if err != nil || len(files) != 1 {
				t.Fatal(files, err)
			}
			before, _ := os.Stat(path)
			if err := sink.ClearFile(ctx, files[0].ID); err != nil {
				t.Fatal(err)
			}
			after, _ := os.Stat(path)
			if !os.SameFile(before, after) {
				t.Fatal("clear replaced descriptor")
			}
			if _, err := sink.ReadFile(ctx, FileRead{ID: files[0].ID, Limit: 100}); !errors.Is(err, fault.Conflict) {
				t.Fatal("stale generation accepted", err)
			}
			if _, err := sink.Write([]byte("after\n")); err != nil {
				t.Fatal(err)
			}
			files, err = sink.Files(ctx)
			if err != nil {
				t.Fatal(err)
			}
			chunk, err := sink.ReadFile(ctx, FileRead{ID: files[0].ID, Limit: 100})
			if err != nil || string(chunk.Data) != "after\n" {
				t.Fatal(string(chunk.Data), err)
			}
			if err := sink.RotateFile(ctx, files[0].ID); err != nil {
				t.Fatal(err)
			}
			if _, err := sink.Write([]byte("new\n")); err != nil {
				t.Fatal(err)
			}
			files, err = sink.Files(ctx)
			if err != nil || len(files) != 2 {
				t.Fatal(files, err)
			}
			if err := sink.DeleteArchive(ctx, files[0].ID); !errors.Is(err, fault.Invalid) {
				t.Fatal("active unlink accepted", err)
			}
			archive, err := sink.ReadFile(ctx, FileRead{ID: files[1].ID, Limit: 100})
			if err != nil || string(archive.Data) != "after\n" {
				t.Fatal(string(archive.Data), err)
			}
			if err := sink.DeleteArchive(ctx, files[1].ID); err != nil {
				t.Fatal(err)
			}
			current, err := sink.ReadFile(ctx, FileRead{ID: files[0].ID, Limit: 100})
			if err != nil || string(current.Data) != "new\n" {
				t.Fatal(string(current.Data), err)
			}
			if _, err := os.Stat(path + ".foundry-rotation.lock"); err != nil {
				t.Fatal("lock disappeared", err)
			}
		})
	}
}
func TestFileControlsRejectPathsLinksSubstitutionAndCancellation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.jsonl")
	sink, _ := PrepareSink(SinkConfig{Driver: File, Path: path})
	if err := sink.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	files, err := sink.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []FileID{"../outside", "app.jsonl", FileID(strings.Repeat("0", 64))} {
		if err := sink.DeleteArchive(ctx, id); err == nil {
			t.Fatal("unowned selection accepted")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := sink.ClearFile(canceled, files[0].ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".external"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := sink.ClearFile(ctx, files[0].ID); !errors.Is(err, fault.Conflict) {
		t.Fatal("substituted file cleared", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "replacement" {
		t.Fatal("replacement changed")
	}
}
func TestConcurrentFileMaintenanceContinuesWriting(t *testing.T) {
	ctx := context.Background()
	sink, _ := PrepareSink(SinkConfig{Driver: File, Path: filepath.Join(t.TempDir(), "app.jsonl"), Async: AsyncConfig{Enabled: true}})
	if err := sink.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	var writers sync.WaitGroup
	for range 4 {
		writers.Go(func() {
			for range 100 {
				_, _ = sink.Write([]byte("concurrent\n"))
			}
		})
	}
	for range 5 {
		files, err := sink.Files(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := sink.RotateFile(ctx, files[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	writers.Wait()
	files, err := sink.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Write([]byte("healthy\n")); err != nil {
		t.Fatal(err)
	}
	chunk, err := sink.ReadFile(ctx, FileRead{ID: files[0].ID, Offset: -1, Limit: 4096})
	if err != nil || !strings.Contains(string(chunk.Data), "healthy") {
		t.Fatal(err)
	}
}
