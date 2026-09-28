package http

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

type downloadSizedZeroReader struct {
	size, position int64
	closed         bool
}

func (r *downloadSizedZeroReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, errors.New("closed source")
	}
	if r.position == r.size {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.size-r.position {
		n = int(r.size - r.position)
	}
	clear(p[:n])
	r.position += int64(n)
	return n, nil
}
func (r *downloadSizedZeroReader) Seek(offset int64, whence int) (int64, error) {
	position := offset
	switch whence {
	case io.SeekEnd:
		position += r.size
	case io.SeekCurrent:
		position += r.position
	case io.SeekStart:
	default:
		return 0, errors.New("invalid seek")
	}
	if position < 0 || position > r.size {
		return 0, errors.New("invalid position")
	}
	r.position = position
	return position, nil
}
func (r *downloadSizedZeroReader) Close() error { r.closed = true; return nil }

type downloadCountingWriter struct {
	header  stdhttp.Header
	status  int
	bytes   int64
	largest int
}

func (w *downloadCountingWriter) Header() stdhttp.Header { return w.header }
func (w *downloadCountingWriter) WriteHeader(status int) { w.status = status }
func (w *downloadCountingWriter) Write(p []byte) (int, error) {
	w.bytes += int64(len(p))
	if len(p) > w.largest {
		w.largest = len(p)
	}
	return len(p), nil
}

func BenchmarkDownloadResponseStreaming(b *testing.B) {
	for _, tc := range []struct {
		name  string
		bytes int64
	}{{"32KiB", 32 << 10}, {"8MiB", 8 << 20}} {
		b.Run(tc.name, func(b *testing.B) {
			endpoint := downloadEndpoint()
			var opened *downloadSizedZeroReader
			router, err := NewRouter(endpoint.Handle(func(context.Context, downloadRequest) (Download, error) {
				return DownloadFrom(func(context.Context) (DownloadContent, error) {
					opened = &downloadSizedZeroReader{size: tc.bytes}
					return DownloadContent{Body: opened, MediaType: "text/plain; charset=utf-8"}, nil
				}), nil
			}))
			if err != nil {
				b.Fatal(err)
			}
			request := httptest.NewRequestWithContext(b.Context(), "GET", "/file", nil)
			b.ReportAllocs()
			b.SetBytes(tc.bytes)
			for b.Loop() {
				writer := &downloadCountingWriter{header: make(stdhttp.Header)}
				router.ServeHTTP(writer, request)
				if writer.status != 200 || writer.bytes != tc.bytes || writer.largest > 32<<10 || !opened.closed {
					b.Fatal("streaming response changed transfer or cleanup bounds", writer.status, writer.bytes, writer.largest)
				}
			}
		})
	}
}
