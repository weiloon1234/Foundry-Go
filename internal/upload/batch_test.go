package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func testBatch(t *testing.T, ctx context.Context, change func(*Config)) *Batch {
	t.Helper()
	directory := t.TempDir()
	config := Config{TempDirectory: directory, MaxBytes: 128 << 10, MaxFileBytes: 64 << 10, MaxFiles: 4, MaxReaders: 2}
	if change != nil {
		change(&config)
	}
	batch, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := batch.Close(); err != nil {
			t.Errorf("close upload batch: %v", err)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Errorf("temporary files remain: count=%d, error=%v", len(entries), err)
		}
	})
	return batch
}

func captureFile(t *testing.T, batch *Batch, content string) File {
	t.Helper()
	file, err := batch.Capture(context.Background(), strings.NewReader(content), `C:\fakepath\photo.TXT`, "image/png; ignored=true")
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestCaptureOwnsMetadataReadersAndCleanup(t *testing.T) {
	batch := testBatch(t, context.Background(), nil)
	if entries, err := os.ReadDir(batch.config.TempDirectory); err != nil || len(entries) != 0 {
		t.Fatal("batch created files before capture")
	}
	file := captureFile(t, batch, "hello world")
	if file.IsZero() || file.Name() != "photo.TXT" || file.Extension() != "txt" || file.Size() != 11 || file.ClientContentType() != "image/png" || file.ContentType() != "text/plain; charset=utf-8" {
		t.Fatal("file metadata was not captured independently of its declared media type")
	}
	if _, err := json.Marshal(file); err == nil {
		t.Fatal("upload was implicitly serialized")
	}
	reader, err := file.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reader.(*os.File); ok {
		t.Fatal("raw temporary file escaped")
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "hello world" {
		t.Fatalf("read %q: %v", data, err)
	}
	if _, err := reader.Seek(6, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(reader)
	if err != nil || string(data) != "world" {
		t.Fatalf("seek read %q: %v", data, err)
	}
	second, err := file.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(second)
	if err != nil || string(data) != "hello world" {
		t.Fatal("new reader did not start at zero")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := batch.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("retained reader after cleanup: %v", err)
	}
	if _, err := file.Open(context.Background()); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("open after cleanup: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if file.Name() != "photo.TXT" || file.Size() != 11 {
		t.Fatal("cleanup lost immutable metadata")
	}
}

func TestZeroByteFileRemainsPresent(t *testing.T) {
	batch := testBatch(t, context.Background(), nil)
	file, err := batch.Capture(context.Background(), strings.NewReader(""), "", "")
	if err != nil || file.IsZero() || file.Size() != 0 || file.Name() != "upload" {
		t.Fatalf("empty file was treated as absent: %v", err)
	}
	reader, err := file.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data, err := io.ReadAll(reader); err != nil || len(data) != 0 {
		t.Fatalf("empty contents: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	var absent File
	if !absent.IsZero() {
		t.Fatal("zero handle is present")
	}
	if _, err := absent.Open(context.Background()); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("absent file opened: %v", err)
	}
}

func TestCaptureLimitsRemovePartialFiles(t *testing.T) {
	for _, test := range []struct {
		name   string
		config Config
		inputs []string
		kind   Limit
	}{
		{"file", Config{MaxBytes: 8, MaxFileBytes: 3, MaxFiles: 3, MaxReaders: 1}, []string{"1234"}, FileBytes},
		{"total", Config{MaxBytes: 5, MaxFileBytes: 4, MaxFiles: 3, MaxReaders: 1}, []string{"123", "456"}, TotalBytes},
		{"files", Config{MaxBytes: 8, MaxFileBytes: 4, MaxFiles: 1, MaxReaders: 1}, []string{"", ""}, Files},
	} {
		t.Run(test.name, func(t *testing.T) {
			batch := testBatch(t, context.Background(), func(c *Config) { directory := c.TempDirectory; *c = test.config; c.TempDirectory = directory })
			for _, input := range test.inputs[:len(test.inputs)-1] {
				captureFile(t, batch, input)
			}
			file, err := batch.Capture(context.Background(), strings.NewReader(test.inputs[len(test.inputs)-1]), "partial", "text/plain")
			var limit *LimitError
			if !file.IsZero() || !errors.As(err, &limit) || limit.Kind != test.kind {
				t.Fatalf("expected limit %v without partial file, got %v", test.kind, err)
			}
			entries, readErr := os.ReadDir(batch.dir)
			if readErr != nil || len(entries) != len(test.inputs)-1 {
				t.Fatalf("partial file leaked: entries=%d error=%v", len(entries), readErr)
			}
		})
	}
}

func TestReaderLimitReleasesSlots(t *testing.T) {
	batch := testBatch(t, context.Background(), func(c *Config) { c.MaxReaders = 1 })
	file := captureFile(t, batch, "data")
	first, err := file.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Open(context.Background()); err == nil {
		t.Fatal("reader budget was ignored")
	} else {
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Kind != Readers {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := file.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

type hostileReadError struct{}

func (hostileReadError) Error() string { panic("error formatting must not execute") }

func TestFailedReadersDiscardPartialUploads(t *testing.T) {
	for _, test := range []struct {
		name     string
		read     readFunc
		callback bool
	}{
		{"panic", func([]byte) (int, error) { panic(hostileReadError{}) }, true},
		{"goexit", func([]byte) (int, error) { runtime.Goexit(); return 0, nil }, true},
		{"read-error", func(p []byte) (int, error) { return copy(p, "partial"), hostileReadError{} }, false},
		{"no-progress", func([]byte) (int, error) { return 0, nil }, false},
		{"negative-count", func([]byte) (int, error) { return -1, nil }, false},
		{"oversized-count", func(p []byte) (int, error) { return len(p) + 1, nil }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			batch := testBatch(t, context.Background(), nil)
			file, err := batch.Capture(context.Background(), test.read, "bad", "")
			if err == nil || !file.IsZero() {
				t.Fatal("failed reader published a file")
			}
			_ = err.Error() // Does not call an arbitrary reader error's formatter.
			if test.callback && !errors.Is(err, fault.Panicked) {
				t.Fatalf("callback failure was not owned: %v", err)
			}
			entries, readErr := os.ReadDir(batch.dir)
			if readErr != nil || len(entries) != 0 {
				t.Fatal("partial capture survived a reader failure")
			}
		})
	}
}

func TestCancellationAndCloseWaitForOwnedCapture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batch := testBatch(t, ctx, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	type result struct {
		file File
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		file, err := batch.Capture(ctx, readFunc(func(p []byte) (int, error) { close(entered); <-release; return copy(p, "late"), io.EOF }), "late", "")
		finished <- result{file, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("capture did not enter reader")
	}
	cancel()
	closed := make(chan error, 1)
	go func() { closed <- batch.Close() }()
	select {
	case <-finished:
		t.Fatal("capture abandoned an active reader")
	case <-closed:
		t.Fatal("cleanup abandoned an active reader")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case result := <-finished:
		if !result.file.IsZero() || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("canceled capture returned %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("capture did not finish")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not finish")
	}
}

func TestRequestAndOperationContextsGovernReaders(t *testing.T) {
	for _, requestContext := range []bool{false, true} {
		t.Run(map[bool]string{false: "operation", true: "request"}[requestContext], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			batchContext, readerContext := context.Background(), ctx
			if requestContext {
				batchContext, readerContext = ctx, context.Background()
			}
			batch := testBatch(t, batchContext, nil)
			file := captureFile(t, batch, "data")
			reader, err := file.Open(readerContext)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
				t.Fatalf("read ignored cancellation: %v", err)
			}
			if _, err := reader.Seek(0, io.SeekStart); !errors.Is(err, context.Canceled) {
				t.Fatalf("seek ignored cancellation: %v", err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOwnedRootCannotFollowOutsideSymlink(t *testing.T) {
	batch := testBatch(t, context.Background(), nil)
	file := captureFile(t, batch, "secret")
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(batch.dir, file.key)
	if err := os.Remove(owned); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, owned); err != nil {
		t.Fatal(err)
	}
	if reader, err := file.Open(context.Background()); err == nil {
		reader.Close()
		t.Fatal("upload root followed an outside symlink")
	}
	if err := batch.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(outside); err != nil || !bytes.Equal(data, []byte("secret")) {
		t.Fatal("cleanup changed a file outside its owned root")
	}
}

func TestCleanupPreservesUnownedDirectoryEntries(t *testing.T) {
	directory := t.TempDir()
	batch, err := New(context.Background(), Config{TempDirectory: directory, MaxBytes: 8, MaxFileBytes: 8, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		t.Fatal(err)
	}
	file := captureFile(t, batch, "owned")
	extra := filepath.Join(batch.dir, "not-owned")
	if err := os.WriteFile(extra, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := batch.Close(); err == nil {
		t.Fatal("cleanup concealed an unexpected directory entry")
	}
	if _, err := os.Stat(filepath.Join(batch.dir, file.key)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("owned file remained: %v", err)
	}
	if data, err := os.ReadFile(extra); err != nil || string(data) != "retain" {
		t.Fatal("cleanup deleted an unowned entry")
	}
}

type boundedSource struct {
	remaining int
	largest   int
}

func (r *boundedSource) Read(p []byte) (int, error) {
	r.largest = max(r.largest, len(p))
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	clear(p[:n])
	r.remaining -= n
	return n, nil
}

func TestCaptureUsesBoundedReaderBuffer(t *testing.T) {
	const size = 8 << 20
	batch := testBatch(t, context.Background(), func(c *Config) { c.MaxBytes = size; c.MaxFileBytes = size })
	source := &boundedSource{remaining: size}
	file, err := batch.Capture(context.Background(), source, "large.bin", "application/octet-stream")
	if err != nil || file.Size() != size || source.largest > bufferBytes {
		t.Fatalf("capture size=%d read buffer=%d error=%v", file.Size(), source.largest, err)
	}
	reader, err := file.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count, err := io.Copy(io.Discard, reader)
	if err != nil || count != size {
		t.Fatalf("captured stream size=%d error=%v", count, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentReadersAndCleanup(t *testing.T) {
	batch := testBatch(t, context.Background(), func(c *Config) { c.MaxReaders = 16 })
	file := captureFile(t, batch, strings.Repeat("data", 1024))
	start := make(chan struct{})
	failures := make(chan error, 16)
	var workers sync.WaitGroup
	for range 8 {
		reader, err := file.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		workers.Go(func() {
			<-start
			buffer := make([]byte, 31)
			for {
				_, err := reader.Read(buffer)
				if err != nil {
					if err != io.EOF && !errors.Is(err, fs.ErrClosed) {
						failures <- err
					}
					break
				}
			}
			if err := reader.Close(); err != nil {
				failures <- err
			}
		})
	}
	close(start)
	if err := batch.Close(); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("concurrent reader cleanup: %v", err)
	}
}
