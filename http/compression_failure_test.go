package http

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

type compressionReaderFunc func([]byte) (int, error)

func (f compressionReaderFunc) Read(p []byte) (int, error) { return f(p) }

func expectCompressionAbort(t *testing.T, handler stdhttp.Handler, writer stdhttp.ResponseWriter, request *stdhttp.Request) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != stdhttp.ErrAbortHandler {
			t.Errorf("incomplete response did not abort: %v", recovered)
		}
	}()
	handler.ServeHTTP(writer, request)
}

func TestCompressionRetainsSourceFailureWhenHandlerIgnoresIt(t *testing.T) {
	sourceFailure := errors.New("source failed after prefix")
	for _, minimum := range []int{0, 64 << 10} {
		for _, kind := range []string{"source-error", "negative-count", "oversized-count", "no-progress"} {
			t.Run(strconv.Itoa(minimum)+"/"+kind, func(t *testing.T) {
				config := DefaultCompressionConfig()
				config.MinBytes = minimum
				var observed error
				handler := compressionTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
					w.Header().Set("Content-Type", "text/plain")
					source := compressionReaderFunc(func(p []byte) (int, error) {
						switch kind {
						case "source-error":
							return copy(p, "accepted prefix"), sourceFailure
						case "negative-count":
							return -1, nil
						case "oversized-count":
							return len(p) + 1, nil
						default:
							return 0, nil
						}
					})
					_, observed = io.Copy(w, source)
					if observed == nil {
						t.Error("source failure disappeared")
					}
					if kind == "source-error" && !errors.Is(observed, sourceFailure) {
						t.Error("source cause disappeared")
					}
					if kind == "no-progress" && !errors.Is(observed, io.ErrNoProgress) {
						t.Error("no-progress cause disappeared")
					}
					if n, err := w.Write([]byte("later")); n != 0 || !errors.Is(err, observed) {
						t.Error("failed response accepted more bytes", n, err)
					}
					reads := 0
					_, err := io.Copy(w, compressionReaderFunc(func([]byte) (int, error) { reads++; return 0, io.EOF }))
					if reads != 0 || !errors.Is(err, observed) {
						t.Error("failed response read another source", reads, err)
					}
					// Deliberately return after ignoring the error; middleware still owns failure.
				})
				request := httptest.NewRequest("GET", "/", nil)
				request.Header.Set("Accept-Encoding", "gzip")
				recorder := httptest.NewRecorder()
				expectCompressionAbort(t, handler, recorder, request)
				if minimum != 0 && recorder.Body.Len() != 0 {
					t.Error("failed buffered prefix was committed")
				}
				if recorder.Body.Len() != 0 {
					reader, err := gzip.NewReader(bytes.NewReader(recorder.Body.Bytes()))
					if err == nil {
						_, err = io.ReadAll(reader)
						reader.Close()
					}
					if err == nil {
						t.Error("failed source produced a complete gzip stream")
					}
				}
			})
		}
	}
}

type compressionInvalidCountWriter struct {
	*httptest.ResponseRecorder
	negative bool
}

func (w compressionInvalidCountWriter) Write(p []byte) (int, error) {
	if w.negative {
		return -1, nil
	}
	return len(p) + 1, nil
}

func TestCompressionRejectsInvalidWriterCountsWithoutAnotherWrite(t *testing.T) {
	for _, negative := range []bool{false, true} {
		handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			n, first := w.Write([]byte("body"))
			if n != 0 || first == nil {
				t.Error("invalid writer count escaped", n, first)
			}
			if n, err := w.Write([]byte("later")); n != 0 || !errors.Is(err, first) {
				t.Error("writer failure was not retained", n, err)
			}
		})
		expectCompressionAbort(t, handler, compressionInvalidCountWriter{httptest.NewRecorder(), negative}, httptest.NewRequest("GET", "/", nil))
	}
}

func TestCompressionRetainsDeclaredLengthFailuresAcrossModes(t *testing.T) {
	for _, minimum := range []int{0, 64 << 10} {
		for _, body := range []string{"", "xy", "too long"} {
			t.Run(strconv.Itoa(minimum)+"/"+strconv.Itoa(len(body)), func(t *testing.T) {
				config := DefaultCompressionConfig()
				config.MinBytes = minimum
				handler := compressionTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
					w.Header().Set("Content-Type", "text/plain")
					w.Header().Set("Content-Length", "5")
					if body != "" {
						_, _ = io.WriteString(w, body)
					}
				})
				request := httptest.NewRequest("GET", "/", nil)
				request.Header.Set("Accept-Encoding", "gzip")
				expectCompressionAbort(t, handler, httptest.NewRecorder(), request)
			})
		}
	}
}
