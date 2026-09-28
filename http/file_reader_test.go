package http

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type fileReaderCallbacks struct {
	read  func([]byte) (int, error)
	seek  func(int64, int) (int64, error)
	close func() error
}

func (b fileReaderCallbacks) Read(p []byte) (int, error) {
	if b.read == nil {
		return 0, io.EOF
	}
	return b.read(p)
}
func (b fileReaderCallbacks) Seek(o int64, w int) (int64, error) {
	if b.seek == nil {
		return o, nil
	}
	return b.seek(o, w)
}
func (b fileReaderCallbacks) Close() error {
	if b.close == nil {
		return nil
	}
	return b.close()
}

func TestFileReaderOwnsFailuresAndRedactsNativeErrors(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"read", "seek", "close"} {
		for _, mode := range []string{"error", "panic", "goexit"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				private := errors.New("private-storage-object-and-credentials")
				fail := func() error {
					switch mode {
					case "panic":
						panic(private)
					case "goexit":
						runtime.Goexit()
					}
					return private
				}
				body := fileReaderCallbacks{}
				switch operation {
				case "read":
					body.read = func([]byte) (int, error) { return 0, fail() }
				case "seek":
					body.seek = func(int64, int) (int64, error) { return 0, fail() }
				case "close":
					body.close = fail
				}
				reader, err := newFileReader(t.Context(), body)
				if err != nil {
					t.Fatal(err)
				}
				var wire error
				switch operation {
				case "read":
					_, wire = reader.Read(make([]byte, 1))
				case "seek":
					_, wire = reader.Seek(0, io.SeekStart)
				case "close":
					wire = reader.Close()
				}
				if wire == nil {
					t.Fatal("source failure was lost")
				}
				cause := reader.Failure()
				if operation == "close" {
					cause = wire
				} else if wire != errFileTransfer || strings.Contains(wire.Error(), "private") {
					t.Fatal("private failure exposed to native response", wire)
				}
				if mode == "error" && !errors.Is(cause, private) {
					t.Fatal("private cause identity lost", cause)
				}
				if mode != "error" && !errors.Is(cause, fault.Panicked) {
					t.Fatal("extension failure was not owned", cause)
				}
				if operation != "close" {
					if err := reader.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestFileReaderValidatesNativeReadAndSeekContracts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body fileReaderCallbacks
		seek bool
	}{
		{"negative read", fileReaderCallbacks{read: func([]byte) (int, error) { return -1, nil }}, false},
		{"large read", fileReaderCallbacks{read: func(p []byte) (int, error) { return len(p) + 1, nil }}, false},
		{"negative seek", fileReaderCallbacks{seek: func(int64, int) (int64, error) { return -1, nil }}, true},
		{"incorrect seek", fileReaderCallbacks{seek: func(int64, int) (int64, error) { return 2, nil }}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, err := newFileReader(t.Context(), tc.body)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if tc.seek {
				n, err := reader.Seek(0, io.SeekStart)
				if n != 0 || err != errFileTransfer {
					t.Fatal("invalid seek escaped", n, err)
				}
			} else {
				n, err := reader.Read(make([]byte, 1))
				if n != 0 || err != errFileTransfer {
					t.Fatal("invalid read escaped", n, err)
				}
			}
			if reader.Failure() == nil {
				t.Fatal("missing private diagnostic")
			}
		})
	}
	reader, err := newFileReader(t.Context(), fileReaderCallbacks{read: func([]byte) (int, error) { return 0, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	_, err = io.Copy(io.Discard, reader)
	if err != errFileTransfer || !errors.Is(reader.Failure(), io.ErrNoProgress) {
		t.Fatal("empty reads not bounded", err)
	}
}

func TestFileReaderPreservesOrdinaryEOFAndSeeks(t *testing.T) {
	t.Parallel()
	source := strings.NewReader("abcdef")
	var closes atomic.Int32
	reader, err := newFileReader(t.Context(), fileReaderCallbacks{read: source.Read, seek: source.Seek, close: func() error { closes.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if size, err := reader.Seek(0, io.SeekEnd); size != 6 || err != nil {
		t.Fatal(size, err)
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "abcdef" || reader.Failure() != nil {
		t.Fatal(string(data), err, reader.Failure())
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil || closes.Load() != 1 {
		t.Fatal("close ownership", err, closes.Load())
	}
	if n, err := reader.Read(make([]byte, 1)); n != 0 || err != errFileTransfer {
		t.Fatal("read after release", n, err)
	}
	if _, err := reader.Seek(0, io.SeekStart); err != errFileTransfer {
		t.Fatal("seek after release", err)
	}
}

func TestFileReaderCancellationRetainsReturnedBytesAndCloses(t *testing.T) {
	t.Parallel()
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "during"}[during], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls, closes int
			reader, err := newFileReader(ctx, fileReaderCallbacks{read: func(p []byte) (int, error) { calls++; p[0] = 'x'; cancel(); return 1, nil }, close: func() error { closes++; return nil }})
			if err != nil {
				t.Fatal(err)
			}
			if !during {
				cancel()
			}
			n, err := reader.Read(make([]byte, 1))
			if err != errFileTransfer || !errors.Is(reader.Failure(), context.Canceled) {
				t.Fatal(n, err)
			}
			if during && (n != 1 || calls != 1) || !during && (n != 0 || calls != 0) {
				t.Fatal("cancellation/read count", n, calls)
			}
			if err := reader.Close(); err != nil || closes != 1 {
				t.Fatal("canceled close lost", err, closes)
			}
		})
	}
}

func TestFileReaderCloseWaitsForActiveCallbackAndPreventsLateAccess(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRead := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseRead()
	var calls, closes atomic.Int32
	reader, err := newFileReader(t.Context(), fileReaderCallbacks{read: func([]byte) (int, error) { calls.Add(1); close(entered); <-release; return 0, io.EOF }, close: func() error { closes.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan struct{})
	go func() { defer close(readDone); reader.Read(make([]byte, 1)) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not start")
	}
	closeStarted, closeDone := make(chan struct{}), make(chan struct{})
	go func() { close(closeStarted); defer close(closeDone); reader.Close() }()
	<-closeStarted
	select {
	case <-closeDone:
		t.Fatal("close abandoned active callback")
	case <-time.After(20 * time.Millisecond):
	}
	releaseRead()
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("owned read stuck")
	}
	select {
	case <-closeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("owned close stuck")
	}
	reader.Read(make([]byte, 1))
	reader.Seek(0, io.SeekStart)
	if calls.Load() != 1 || closes.Load() != 1 {
		t.Fatal("source accessed after cleanup", calls.Load(), closes.Load())
	}
}
