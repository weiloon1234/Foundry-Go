package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

type streamRequest = Input[NoPath, NoQuery, NoBody]

func streamEndpoint() Endpoint[NoPath, NoQuery, NoBody, Stream] {
	return DefineEndpoint(DefineRoute(RouteSpec{ID: "reports.stream", Method: GET, Access: Public}, StaticPath("/stream")), EmptyQuery(), EmptyBody(), StreamResponse("text/plain; charset=utf-8"))
}
func streamRouter(t *testing.T, endpoint Endpoint[NoPath, NoQuery, NoBody, Stream], source StreamSource) *Router {
	t.Helper()
	router, err := NewRouter(endpoint.Handle(func(context.Context, streamRequest) (Stream, error) { return StreamFrom(source), nil }))
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func TestStreamKnownUnknownAndHead(t *testing.T) {
	t.Parallel()
	for _, known := range []bool{false, true} {
		for _, size := range []int{0, 7, 65536} {
			for _, method := range []string{"GET", "HEAD"} {
				t.Run(strconv.FormatBool(known)+"/"+strconv.Itoa(size)+"/"+method, func(t *testing.T) {
					var opens, reads, closes atomic.Int32
					payload := strings.Repeat("x", size)
					router := streamRouter(t, streamEndpoint(), func(context.Context) (StreamContent, error) {
						opens.Add(1)
						reader := strings.NewReader(payload)
						content := StreamContent{Body: fileReaderCallbacks{read: func(p []byte) (int, error) { reads.Add(1); return reader.Read(p) }, close: func() error { closes.Add(1); return nil }}, Name: "../report.txt", MediaType: "text/plain; charset=UTF-8"}
						if known {
							content.Length = value.Set(int64(size))
						}
						return content, nil
					})
					request := httptest.NewRequest(method, "/stream", nil)
					// Unseekable sources do not parse or allocate per-range request data.
					request.Header["Range"] = []string{"bytes=1-2", "bytes=3-4"}
					recorder := httptest.NewRecorder()
					recorder.Header().Set("ETag", "\"stale\"")
					recorder.Header().Set("X-Policy", "retained")
					router.ServeHTTP(recorder, request)
					if recorder.Code != 200 || opens.Load() != 1 || closes.Load() != 1 {
						t.Fatal("status/ownership", recorder.Code, opens.Load(), closes.Load())
					}
					expected := payload
					if method == "HEAD" {
						expected = ""
						if reads.Load() != 0 {
							t.Fatal("HEAD consumed source")
						}
					}
					if recorder.Body.String() != expected {
						t.Fatal("stream content differs")
					}
					wantLength := ""
					if known {
						wantLength = strconv.Itoa(size)
					}
					if recorder.Header().Get("Content-Length") != wantLength || recorder.Header().Get("Accept-Ranges") != "none" || recorder.Header().Get("ETag") != "" || recorder.Header().Get("Content-Range") != "" || recorder.Header().Get("X-Policy") != "retained" {
						t.Fatal("headers", recorder.Header())
					}
					media, params, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
					if err != nil || media != "attachment" || params["filename"] != "report.txt" {
						t.Fatal("filename", recorder.Header(), err)
					}
				})
			}
		}
	}
}

func TestStreamRejectsBeforeCommitAndCloses(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"source-error", "no-body", "media", "negative", "too-large", "short", "long", "limit", "zero-overrun", "read-error", "read-panic", "read-goexit", "no-progress", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var closes atomic.Int32
			endpoint := streamEndpoint()
			limits := DefaultEndpointLimits()
			limits.Files.Bytes = 10
			endpoint = endpoint.WithLimits(limits)
			router := streamRouter(t, endpoint, func(context.Context) (StreamContent, error) {
				text := "payload"
				if mode == "limit" {
					text = strings.Repeat("x", 11)
				}
				reader := strings.NewReader(text)
				read := reader.Read
				// This case models a reader reporting final data and EOF in
				// the same first call. Later EOF failures are covered over TCP.
				if mode == "short" {
					read = func(p []byte) (int, error) {
						n, err := reader.Read(p)
						if reader.Len() == 0 {
							return n, io.EOF
						}
						return n, err
					}
				}
				switch mode {
				case "read-error":
					read = func([]byte) (int, error) { return 0, errors.New("private-read-detail") }
				case "read-panic":
					read = func([]byte) (int, error) { panic("private-read-detail") }
				case "read-goexit":
					read = func([]byte) (int, error) { runtime.Goexit(); return 0, nil }
				case "no-progress":
					read = func([]byte) (int, error) { return 0, nil }
				}
				c := StreamContent{Body: fileReaderCallbacks{read: read, close: func() error { closes.Add(1); return nil }}, Name: "private-source-name", MediaType: "text/plain; charset=utf-8"}
				switch mode {
				case "source-error":
					return c, NotFound.WithCause(errors.New("private-provider-detail"))
				case "no-body":
					c.Body = nil
				case "media":
					c.MediaType = "application/pdf"
				case "negative":
					c.Length = value.Set(int64(-1))
				case "too-large":
					c.Length = value.Set(int64(11))
				case "short":
					c.Length = value.Set(int64(9))
				case "long":
					c.Length = value.Set(int64(5))
				case "zero-overrun":
					c.Length = value.Set(int64(0))
				case "canceled":
					cancel()
				}
				return c, nil
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequestWithContext(ctx, "GET", "/stream", nil))
			want := 500
			if mode == "source-error" {
				want = 404
			}
			if mode == "canceled" {
				want = 408
			}
			expectedCloses := int32(1)
			if mode == "no-body" {
				expectedCloses = 0
			}
			if recorder.Code != want || closes.Load() != expectedCloses || strings.Contains(recorder.Body.String(), "private") || strings.Contains(recorder.Body.String(), "payload") || recorder.Header().Get("Content-Disposition") != "" {
				t.Fatal("unsafe failure", mode, recorder.Code, closes.Load(), recorder.Body.String())
			}
		})
	}
}

func TestStreamMetadataIsOwnedAndTypeIsNotJSON(t *testing.T) {
	media := []MediaType{"text/plain"}
	response := StreamResponse("application/pdf", media...)
	media[0] = "invalid"
	endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "metadata", Method: GET, Access: Public}, StaticPath("/meta")), EmptyQuery(), EmptyBody(), response)
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	if info.Response.File.Seekable || len(info.Response.File.MediaTypes) != 2 || info.Response.File.MediaTypes[1] != "text/plain" {
		t.Fatal(info)
	}
	info.Response.File.MediaTypes[0] = "changed"
	next, err := endpoint.Description()
	if err != nil || next.Response.File.MediaTypes[0] != "application/pdf" {
		t.Fatal("metadata leaked", err)
	}
	if _, err := json.Marshal(StreamFrom(func(context.Context) (StreamContent, error) {
		t.Fatal("JSON opened source")
		return StreamContent{}, nil
	})); err == nil {
		t.Fatal("stream serialized as DTO")
	}
	if StreamResponse("text/*").Validate() == nil || StreamResponse("text/plain", "TEXT/PLAIN").Validate() == nil || (Stream{}).Validate() == nil {
		t.Fatal("invalid declaration accepted")
	}
	var closes atomic.Int32
	base := StreamFrom(func(context.Context) (StreamContent, error) {
		return StreamContent{Body: fileReaderCallbacks{read: strings.NewReader("ok").Read, close: func() error { closes.Add(1); return nil }}, MediaType: "application/pdf", Name: "original"}, nil
	})
	changed := base.WithName("résumé.txt").WithDisposition(DispositionInline).WithMediaType("text/plain; charset=utf-8")
	router, err := NewRouter(streamEndpoint().Handle(func(context.Context, streamRequest) (Stream, error) { return changed, nil }))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/stream", nil))
	disposition, params, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
	if recorder.Code != 200 || err != nil || disposition != "inline" || params["filename"] != "résumé.txt" || closes.Load() != 1 {
		t.Fatal(recorder.Header(), err)
	}
	if _, set := base.name.Get(); set || base.disposition != DispositionAttachment {
		t.Fatal("modifiers mutated original")
	}
}

func TestStreamOwnsWriterAndCloseFailures(t *testing.T) {
	for _, operation := range []string{"header", "status", "write", "close"} {
		for _, mode := range []string{"panic", "goexit"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				var calls, closes atomic.Int32
				fail := func() {
					calls.Add(1)
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("private-failure")
				}
				router := streamRouter(t, streamEndpoint(), func(context.Context) (StreamContent, error) {
					return StreamContent{Body: fileReaderCallbacks{read: strings.NewReader("ok").Read, close: func() error {
						closes.Add(1)
						if operation == "close" {
							fail()
						}
						return nil
					}}, MediaType: "text/plain; charset=utf-8"}, nil
				})
				writer := downloadCallbackWriter{ResponseRecorder: httptest.NewRecorder()}
				switch operation {
				case "header":
					writer.header = fail
				case "status":
					writer.status = fail
				case "write":
					writer.write = fail
				}
				defer func() {
					got := recover()
					if operation == "close" {
						if got != nil || writer.Body.String() != "ok" {
							t.Error("close replaced response", got)
						}
					} else if got != stdhttp.ErrAbortHandler {
						t.Error("writer was not aborted", got)
					}
					if closes.Load() != 1 || calls.Load() != 1 {
						t.Error("ownership", closes.Load(), calls.Load())
					}
				}()
				router.ServeHTTP(writer, httptest.NewRequest("GET", "/stream", nil))
			})
		}
	}
}

func TestStreamRequiresEOFBeforeLastDeclaredChunk(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(strconv.Itoa(extra), func(t *testing.T) {
			source := strings.NewReader(strings.Repeat("x", 65536+extra))
			var closes atomic.Int32
			router := streamRouter(t, streamEndpoint(), func(context.Context) (StreamContent, error) {
				return StreamContent{Body: fileReaderCallbacks{read: source.Read, close: func() error { closes.Add(1); return nil }}, Length: value.Set(int64(65536)), MediaType: "text/plain; charset=utf-8"}, nil
			})
			writer := httptest.NewRecorder()
			defer func() {
				got := recover()
				if extra == 0 {
					if got != nil || writer.Body.Len() != 65536 {
						t.Error("valid exact length rejected", got)
					}
				} else if got != stdhttp.ErrAbortHandler || writer.Body.Len() >= 65536 {
					t.Error("overrun looked complete", got, writer.Body.Len())
				}
				if closes.Load() != 1 {
					t.Error("body leaked")
				}
			}()
			router.ServeHTTP(writer, httptest.NewRequest("GET", "/stream", nil))
		})
	}
}

// An io.Reader may report final data and EOF together; neither is discarded.
func TestStreamRetainsDataReturnedWithEOF(t *testing.T) {
	for _, known := range []bool{false, true} {
		router := streamRouter(t, streamEndpoint(), func(context.Context) (StreamContent, error) {
			c := StreamContent{Body: fileReaderCallbacks{read: func(p []byte) (int, error) { return copy(p, "done"), io.EOF }, close: func() error { return nil }}, MediaType: "text/plain; charset=utf-8"}
			if known {
				c.Length = value.Set(int64(4))
			}
			return c, nil
		})
		writer := httptest.NewRecorder()
		router.ServeHTTP(writer, httptest.NewRequest("GET", "/stream", nil))
		if writer.Code != 200 || writer.Body.String() != "done" {
			t.Fatal(writer.Code, writer.Body.String())
		}
	}
}

type streamFailingWriter struct {
	*httptest.ResponseRecorder
	cause error
}

func (w streamFailingWriter) Write([]byte) (int, error) { return 0, w.cause }

func TestStreamPreservesTransportFailureCause(t *testing.T) {
	cause := errors.New("private-native-writer-cause")
	response := StreamResponse("text/plain")
	prepared, err := response.prepare(t.Context(), StreamFrom(func(context.Context) (StreamContent, error) {
		return StreamContent{Body: io.NopCloser(strings.NewReader("content")), MediaType: "text/plain"}, nil
	}), DefaultEndpointLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.cleanup()()
	writer := streamFailingWriter{ResponseRecorder: httptest.NewRecorder(), cause: cause}
	err = response.write(writer, httptest.NewRequest("GET", "/stream", nil), prepared)
	if !errors.Is(err, cause) {
		t.Fatal("transport cause was replaced by the public I/O marker", err)
	}
	if strings.Contains(writer.Body.String(), "private") {
		t.Fatal("transport cause leaked to response")
	}
}
