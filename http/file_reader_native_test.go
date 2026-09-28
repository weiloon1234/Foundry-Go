package http

import (
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Exercise the actual native multipart producer: its separate goroutine must
// see ordinary, safe errors even when custom I/O panics or exits its goroutine.
func TestFileReaderContainsNativeRangeSourceFailures(t *testing.T) {
	t.Parallel()
	for _, requestRange := range []string{"", "bytes=1-2", "bytes=0-1,4-5"} {
		for _, operation := range []string{"read", "seek"} {
			for _, mode := range []string{"error", "panic", "goexit"} {
				t.Run(requestRange+"/"+operation+"/"+mode, func(t *testing.T) {
					source := strings.NewReader("abcdefghij")
					private := errors.New("private-object-path-and-access-key")
					fail := func() error {
						switch mode {
						case "panic":
							panic(private)
						case "goexit":
							runtime.Goexit()
						}
						return private
					}
					body := fileReaderCallbacks{read: source.Read, seek: source.Seek}
					if operation == "read" {
						body.read = func([]byte) (int, error) { return 0, fail() }
					} else {
						body.seek = func(offset int64, whence int) (int64, error) {
							// Allow size discovery and rewind, but reject the first nonzero range.
							if requestRange != "" && (whence != io.SeekStart || offset == 0) {
								return source.Seek(offset, whence)
							}
							return 0, fail()
						}
					}
					reader, err := newFileReader(t.Context(), body)
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Close()
					request := httptest.NewRequest("GET", "/download", nil)
					request.Header.Set("Range", requestRange)
					recorder := httptest.NewRecorder()
					recorder.Header().Set("Content-Type", "application/octet-stream")
					done := make(chan struct{})
					go func() {
						defer close(done)
						stdhttp.ServeContent(recorder, request, "document.bin", time.Time{}, reader)
					}()
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Fatal("native range producer failed to terminate")
					}
					if reader.Failure() == nil {
						t.Fatal("native discarded the copy error without an owned diagnostic")
					}
					if strings.Contains(recorder.Body.String(), "private") {
						t.Fatal("source details escaped in native response")
					}
				})
			}
		}
	}
}

type fileZeroReader struct{}

func (fileZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type fileDiscardWriter struct{}

func (fileDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkFileReaderStreaming(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int64
	}{{"32KiB", 32 << 10}, {"8MiB", 8 << 20}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(tc.size)
			for b.Loop() {
				reader, err := newFileReader(b.Context(), io.NopCloser(io.LimitReader(fileZeroReader{}, tc.size)))
				if err != nil {
					b.Fatal(err)
				}
				n, err := io.CopyBuffer(fileDiscardWriter{}, reader, make([]byte, 32<<10))
				if err != nil || n != tc.size {
					b.Fatal(n, err)
				}
				if err := reader.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
