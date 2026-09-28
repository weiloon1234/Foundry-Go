package http

import (
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestCompressionAndETagUnsupportedFlushPreservesResponse(t *testing.T) {
	prefix, suffix := "prefix", strings.Repeat("suffix", 256)
	for _, coding := range []string{"gzip", "br"} {
		for _, minimum := range []int{0, 1024} {
			for _, etagFirst := range []bool{false, true} {
				for _, transparent := range []bool{false, true} {
					name := coding + "/" + strconv.Itoa(minimum) + "/etag-first=" + strconv.FormatBool(etagFirst) + "/transparent=" + strconv.FormatBool(transparent)
					t.Run(name, func(t *testing.T) {
						native := &compressionPlainWriter{header: make(stdhttp.Header)}
						var supplied stdhttp.ResponseWriter = native
						if transparent {
							supplied = compressionTransparent{supplied}
						}
						application := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
							w.Header().Set("Content-Type", "text/plain")
							if _, err := io.WriteString(w, prefix); err != nil {
								t.Fatal(err)
							}
							if _, ok := w.(stdhttp.Flusher); ok {
								t.Fatal("nested wrappers invented native flush support")
							}
							if err := stdhttp.NewResponseController(w).Flush(); !errors.Is(err, stdhttp.ErrNotSupported) {
								t.Fatal("unsupported native flush changed its error", err)
							}
							if native.status != 0 || native.body.Len() != 0 {
								t.Fatal("unsupported flush committed buffered output")
							}
							if _, err := io.WriteString(w, suffix); err != nil {
								t.Fatal("unsupported flush prevented later writes", err)
							}
						})
						config := DefaultCompressionConfig()
						config.MinBytes = minimum
						middlewares := []Middleware{ETags(DefaultETagConfig()), Compression(config)}
						if !etagFirst {
							middlewares[0], middlewares[1] = middlewares[1], middlewares[0]
						}
						handler, err := ApplyMiddleware(application, middlewares...)
						if err != nil {
							t.Fatal(err)
						}
						request := httptest.NewRequest("GET", "/", nil)
						request.Header.Set("Accept-Encoding", coding)
						handler.ServeHTTP(supplied, request)
						if native.status != 200 || native.header.Get("Content-Encoding") != coding || native.header.Get("ETag") == "" {
							t.Fatal("unsupported flush changed the final response", native.status, native.header)
						}
						if string(decodeCompressed(t, coding, native.body.Bytes())) != prefix+suffix {
							t.Fatal("unsupported flush lost or duplicated body bytes")
						}
					})
				}
			}
		}
	}
}
