package http

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type downloadRequest = Input[NoPath, NoQuery, NoBody]

func downloadEndpoint() Endpoint[NoPath, NoQuery, NoBody, Download] {
	return DefineEndpoint(DefineRoute(RouteSpec{ID: "files.show", Method: GET, Access: Public}, StaticPath("/file")), EmptyQuery(), EmptyBody(), DownloadResponse("text/plain; charset=utf-8"))
}
func downloadRouter(t *testing.T, endpoint Endpoint[NoPath, NoQuery, NoBody, Download], handler Handler[NoPath, NoQuery, NoBody, Download]) *Router {
	t.Helper()
	router, err := NewRouter(endpoint.Handle(handler))
	if err != nil {
		t.Fatal(err)
	}
	return router
}
func downloadText(text string, closes *atomic.Int32) Download {
	return DownloadFrom(func(context.Context) (DownloadContent, error) {
		source := strings.NewReader(text)
		return DownloadContent{Body: fileReaderCallbacks{read: source.Read, seek: source.Seek, close: func() error { closes.Add(1); return nil }}, Name: "../résumé.txt", MediaType: "text/plain; charset=UTF-8", EntityTag: "\"version-1\"", Modified: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)}, nil
	})
}

func TestDownloadEndpointNativeConditionsAndRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, header, value string
		status                      int
		body, rangeValue            string
	}{
		{"full", "GET", "", "", 200, "abcdefghij", ""},
		{"head", "HEAD", "", "", 200, "", ""},
		{"range", "GET", "Range", "bytes=2-5", 206, "cdef", "bytes 2-5/10"},
		{"suffix", "GET", "Range", "bytes=-3", 206, "hij", "bytes 7-9/10"},
		{"etag", "GET", "If-None-Match", "\"version-1\"", 304, "", ""},
		{"modified", "GET", "If-Modified-Since", "Thu, 02 Jan 2020 03:04:05 GMT", 304, "", ""},
		{"if-match", "GET", "If-Match", "\"other\"", 412, "", ""},
		{"unmodified", "GET", "If-Unmodified-Since", "Wed, 01 Jan 2020 03:04:05 GMT", 412, "", ""},
		{"unsatisfied", "GET", "Range", "bytes=20-30", 416, "", "bytes */10"},
		{"malformed", "GET", "Range", "bytes=invalid", 416, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var closes atomic.Int32
			router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
				return downloadText("abcdefghij", &closes), nil
			})
			request := httptest.NewRequest(tc.method, "/file", nil)
			if tc.header != "" {
				request.Header.Set(tc.header, tc.value)
			}
			recorder := httptest.NewRecorder()
			recorder.Header().Set("X-Policy", "retained")
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.status || closes.Load() != 1 {
				t.Fatal("status/ownership", recorder.Code, closes.Load(), recorder.Body.String())
			}
			if recorder.Header().Get("X-Policy") != "retained" || recorder.Header().Get("Content-Range") != tc.rangeValue {
				t.Fatal("response headers", recorder.Header())
			}
			if tc.status >= 400 {
				failure := decodeFailure(t, recorder)
				expected := RangeNotSatisfiable
				if tc.status == 412 {
					expected = PreconditionFailed
				}
				if failure.Code != expected || recorder.Header().Get("Content-Disposition") != "" {
					t.Fatal("native error bypassed shared errors", recorder.Body.String())
				}
			} else if recorder.Body.String() != tc.body {
				t.Fatal("payload", recorder.Body.String())
			}
			if tc.status == 200 || tc.status == 206 {
				disposition, parameters, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
				if err != nil || disposition != "attachment" || parameters["filename"] != "résumé.txt" {
					t.Fatal("download filename", recorder.Header(), err)
				}
				if recorder.Header().Get("Content-Type") != "text/plain; charset=utf-8" || recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("declared media lost")
				}
			}
		})
	}
}

func TestDownloadEndpointMultipleRangesAndIfRange(t *testing.T) {
	t.Parallel()
	for _, validator := range []string{"", "\"version-1\"", "\"other\""} {
		t.Run(validator, func(t *testing.T) {
			var closes atomic.Int32
			router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
				return downloadText("abcdefghij", &closes), nil
			})
			request := httptest.NewRequest("GET", "/file", nil)
			request.Header.Set("Range", "bytes=0-1,7-9")
			request.Header.Set("If-Range", validator)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if closes.Load() != 1 {
				t.Fatal("source leaked")
			}
			if validator == "\"other\"" {
				if recorder.Code != 200 || recorder.Body.String() != "abcdefghij" {
					t.Fatal("stale validator used range")
				}
				return
			}
			media, parameters, err := mime.ParseMediaType(recorder.Header().Get("Content-Type"))
			if err != nil || media != "multipart/byteranges" || recorder.Code != 206 {
				t.Fatal("range framing", recorder.Header(), err)
			}
			reader := multipart.NewReader(strings.NewReader(recorder.Body.String()), parameters["boundary"])
			for _, expected := range []string{"ab", "hij"} {
				part, err := reader.NextPart()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(part)
				if err != nil || string(data) != expected {
					t.Fatal(string(data), err)
				}
			}
			if _, err := reader.NextPart(); err != io.EOF {
				t.Fatal("unexpected range", err)
			}
			size, err := strconv.Atoi(recorder.Header().Get("Content-Length"))
			if err != nil || size != recorder.Body.Len() {
				t.Fatal("native multipart size diverged", size, recorder.Body.Len())
			}
		})
	}
}

func TestDownloadEndpointFailureClosesTransferredBodiesBeforeCommit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"source-error", "no-body", "media", "tag", "size", "growing-size", "seek-error", "seek-panic", "seek-goexit"} {
		t.Run(mode, func(t *testing.T) {
			var closes atomic.Int32
			download := DownloadFrom(func(context.Context) (DownloadContent, error) {
				source := strings.NewReader("abcdefghij")
				body := fileReaderCallbacks{read: source.Read, seek: source.Seek, close: func() error { closes.Add(1); return nil }}
				content := DownloadContent{Body: body, Name: "private-source-name", MediaType: "text/plain; charset=utf-8"}
				switch mode {
				case "source-error":
					return content, NotFound.WithCause(errors.New("private-storage-detail"))
				case "no-body":
					content.Body = nil
				case "media":
					content.MediaType = "application/pdf"
				case "tag":
					content.EntityTag = "private-unquoted-tag"
				case "growing-size":
					ends := 0
					body.seek = func(offset int64, whence int) (int64, error) {
						if whence == io.SeekEnd {
							ends++
							if ends > 1 {
								return 20, nil
							}
						}
						return source.Seek(offset, whence)
					}
					content.Body = body
				case "seek-error", "seek-panic", "seek-goexit":
					body.seek = func(int64, int) (int64, error) {
						if mode == "seek-panic" {
							panic("private-storage-detail")
						}
						if mode == "seek-goexit" {
							runtime.Goexit()
						}
						return 0, errors.New("private-storage-detail")
					}
					content.Body = body
				}
				return content, nil
			})
			endpoint := downloadEndpoint()
			limits := DefaultEndpointLimits()
			if mode == "size" {
				limits.Files.Bytes = 5
			}
			if mode == "growing-size" {
				limits.Files.Bytes = 10
			}
			endpoint = endpoint.WithLimits(limits)
			router := downloadRouter(t, endpoint, func(context.Context, downloadRequest) (Download, error) { return download, nil })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/file", nil))
			expectedStatus := 500
			if mode == "source-error" {
				expectedStatus = 404
			}
			expectedCloses := int32(1)
			if mode == "no-body" {
				expectedCloses = 0
			}
			if recorder.Code != expectedStatus || closes.Load() != expectedCloses || strings.Contains(recorder.Body.String(), "private") {
				t.Fatal("failure escaped ownership", recorder.Code, closes.Load(), recorder.Body.String())
			}
			if recorder.Header().Get("Content-Disposition") != "" || recorder.Header().Get("Accept-Ranges") != "" {
				t.Fatal("failed representation headers retained")
			}
		})
	}
}

func TestDownloadEndpointAbortsTruncatedSourceAndCloses(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"short-source", "read-error", "read-panic", "read-goexit"} {
		t.Run(mode, func(t *testing.T) {
			var closes atomic.Int32
			source := strings.NewReader("abcdefghij")
			body := fileReaderCallbacks{seek: source.Seek, close: func() error { closes.Add(1); return nil }}
			short := strings.NewReader("short")
			body.read = func(p []byte) (int, error) {
				switch mode {
				case "short-source":
					return short.Read(p)
				case "read-panic":
					panic("private-reader-detail")
				case "read-goexit":
					runtime.Goexit()
				}
				return 0, errors.New("private-reader-detail")
			}
			router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
				return DownloadFrom(func(context.Context) (DownloadContent, error) {
					return DownloadContent{Body: body, MediaType: "text/plain; charset=utf-8"}, nil
				}), nil
			})
			recorder := httptest.NewRecorder()
			defer func() {
				if recovered := recover(); recovered != stdhttp.ErrAbortHandler {
					t.Errorf("failed committed transfer was not aborted: %v", recovered)
				}
				if closes.Load() != 1 || recorder.Code != 200 || strings.Contains(recorder.Body.String(), "private") {
					t.Error("failed transfer lost ownership or replaced its body")
				}
			}()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/file", nil))
		})
	}
}

func TestDownloadEndpointAbortsShortNativeWriterAndCloses(t *testing.T) {
	t.Parallel()
	var closes atomic.Int32
	router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
		return downloadText("abcdefghij", &closes), nil
	})
	writer := &endpointShortWriter{header: make(stdhttp.Header)}
	defer func() {
		if recovered := recover(); recovered != stdhttp.ErrAbortHandler {
			t.Errorf("short writer did not abort: %v", recovered)
		}
		if closes.Load() != 1 || len(writer.statuses) != 1 || writer.statuses[0] != 200 {
			t.Error("short writer lost cleanup or committed twice")
		}
	}()
	router.ServeHTTP(writer, httptest.NewRequest("GET", "/file", nil))
}

func TestDownloadEndpointBoundsRangesBeforeHandlerOrSource(t *testing.T) {
	t.Parallel()
	for _, ranges := range [][]string{{"bytes=0-1", "bytes=2-3"}, {"bytes=0-1,2-3,4-5"}, {strings.Repeat("x", 33)}} {
		endpoint := downloadEndpoint()
		limits := DefaultEndpointLimits()
		limits.Files.Ranges = 2
		limits.Files.RangeBytes = 32
		endpoint = endpoint.WithLimits(limits)
		router := downloadRouter(t, endpoint, func(context.Context, downloadRequest) (Download, error) {
			t.Error("oversized range reached handler")
			return Download{}, nil
		})
		request := httptest.NewRequest("GET", "/file", nil)
		request.Header["Range"] = ranges
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != 400 || decodeFailure(t, recorder).Code != BadRequest {
			t.Fatal("range budget not enforced", recorder.Code)
		}
	}
}

func TestDownloadResponseDeclarationAndMetadataRemainTypedAndOwned(t *testing.T) {
	t.Parallel()
	extra := []MediaType{"application/pdf"}
	response := DownloadResponse("text/plain", extra...)
	extra[0] = "bad"
	if err := response.Validate(); err != nil {
		t.Fatal("caller mutated response declaration", err)
	}
	for _, invalid := range []Response[Download]{DownloadResponse("image/*"), DownloadResponse("text/plain", "TEXT/PLAIN"), {}} {
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid download declaration accepted")
		}
	}
	endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "files.multi", Method: GET, Access: Public}, StaticPath("/file")), EmptyQuery(), EmptyBody(), response)
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	if info.Response == nil || info.Response.File == nil || !info.Response.File.Seekable || info.Response.Schema.Root != "" || info.Response.MediaType != "" {
		t.Fatal("file response acquired a false schema", info.Response)
	}
	info.Response.File.MediaTypes[0] = "changed"
	again, err := endpoint.Description()
	if err != nil || again.Response.File.MediaTypes[0] != "text/plain" {
		t.Fatal("metadata mutation changed declaration")
	}
}
