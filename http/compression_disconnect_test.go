package http

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCompressionClientClosesAfterDeclaredBody(t *testing.T) {
	for _, shape := range []string{"single", "no-store", "404", "two-writes", "over-limit", "truncated"} {
		t.Run(shape, func(t *testing.T) {
			complete := shape != "truncated"
			payload := strings.Repeat("page", 7000)
			status := 200
			if shape == "404" {
				status = 404
			}
			var logs bytes.Buffer
			result := make(chan any, 1)
			handler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				length := len(payload)
				if !complete {
					length++
				}
				w.Header().Set("Content-Length", strconv.Itoa(length))
				w.Header().Set("Content-Type", "text/html")
				if shape == "no-store" {
					w.Header().Set("Cache-Control", "no-store")
				}
				w.WriteHeader(status)
				chunks := []string{payload}
				if shape == "two-writes" {
					chunks = []string{payload[:100], payload[100:]}
				}
				for _, chunk := range chunks {
					if _, err := io.WriteString(w, chunk); err != nil {
						t.Error(err)
						return
					}
				}
				if shape != "no-store" {
					if err := stdhttp.NewResponseController(w).Flush(); err != nil {
						t.Error(err)
						return
					}
				}
				// Hold finish until net/http observes the peer closing the real connection.
				select {
				case <-r.Context().Done():
				case <-time.After(10 * time.Second):
					t.Error("peer close did not cancel request")
				}
			})
			etags := DefaultETagConfig()
			if shape == "over-limit" {
				etags.MaxBytes = 1024
			}
			middleware := []Middleware{CacheControl(CachePolicy{Visibility: CachePublic}), ETags(etags), Compression(DefaultCompressionConfig())}
			wrapped, err := ApplyMiddleware(handler, middleware...)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				defer func() { result <- recover() }()
				wrapped.ServeHTTP(w, withRequestLogger(r, slog.New(slog.NewTextHandler(&logs, nil))))
			}))
			defer server.Close()
			conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			response, err := stdhttp.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			body := make([]byte, len(payload))
			if _, err := io.ReadFull(response.Body, body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != status || string(body) != payload {
				t.Fatal("response changed")
			}
			if complete {
				if _, err := response.Body.Read(make([]byte, 1)); err != io.EOF {
					t.Fatalf("declared body incomplete: %v", err)
				}
			}
			conn.Close()
			select {
			case recovered := <-result:
				if complete {
					if recovered != nil || logs.Len() != 0 {
						t.Fatalf("complete response reported failure: panic=%v logs=%s", recovered, &logs)
					}
				} else if recovered != stdhttp.ErrAbortHandler || !strings.Contains(logs.String(), "HTTP compression could not finish") {
					t.Fatalf("incomplete response lost abort: panic=%v logs=%s", recovered, &logs)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("middleware did not finish")
			}
		})
	}
}

func TestCompressionCancellationStillAbortsUnfinishedResponses(t *testing.T) {
	for _, kind := range []string{"buffered", "encoded", "unknown-length", "trailers", "final-trailer", "late-trailer", "head", "chunked", "pending-empty", "write-error"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			handler := compressionTestHandler(t, DefaultCompressionConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Content-Type", "text/plain")
				if kind == "pending-empty" {
					w.Header().Set("Content-Length", "0")
					w.WriteHeader(200)
					cancel()
					return
				}
				if kind == "chunked" {
					w.Header().Set("Transfer-Encoding", "chunked")
				}
				if kind != "unknown-length" {
					w.Header().Set("Content-Length", "4")
				}
				if kind == "trailers" || kind == "final-trailer" {
					w.Header().Set("Trailer", "X-Final")
				}
				if kind == "head" {
					w.WriteHeader(200)
					if err := stdhttp.NewResponseController(w).Flush(); err != nil {
						t.Error(err)
					}
				}
				_, _ = w.Write([]byte("body"))
				if kind == "final-trailer" {
					w.Header().Del("Trailer")
				}
				if kind == "late-trailer" {
					w.Header().Set(stdhttp.TrailerPrefix+"X-Final", "value")
				}
				if kind == "write-error" {
					_, _ = w.Write([]byte("excess"))
				}
				cancel()
			})
			method := "GET"
			if kind == "head" {
				method = "HEAD"
			}
			request := httptest.NewRequest(method, "/", nil).WithContext(ctx)
			if kind == "buffered" {
				request.Header.Set("Accept-Encoding", "gzip")
			}
			if kind == "encoded" {
				request.Header.Set("Accept-Encoding", "gzip, identity;q=0")
			}
			expectCompressionAbort(t, handler, httptest.NewRecorder(), request)
		})
	}
}
