package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/httpdeadline"
)

func uploadDeadlineServer(t *testing.T, handler stdhttp.Handler, kernel time.Duration) (string, *stdhttp.Client) {
	t.Helper()
	if kernel == 0 {
		server := httptest.NewUnstartedServer(handler)
		server.Config.ReadTimeout, server.Config.WriteTimeout = 5*time.Second, 5*time.Second
		server.Start()
		t.Cleanup(server.Close)
		client := server.Client()
		client.Timeout = 2 * time.Second
		return server.Listener.Addr().String(), client
	}
	config := DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.RequestTimeout = kernel
	config.ReadTimeout, config.WriteTimeout = 5*time.Second, 5*time.Second
	server, err := Prepare(handler, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	ready, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	transport := &stdhttp.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return address, &stdhttp.Client{Transport: transport, Timeout: 2 * time.Second}
}

func TestUploadDeadlineInterruptsTricklingMultipartAndReleasesAdmission(t *testing.T) {
	for _, tc := range []struct {
		name        string
		kernel      time.Duration
		compression bool
		trailer     string
	}{
		{"shorter-than-kernel", 5 * time.Second, false, ""},
		{"longer-than-kernel", 100 * time.Millisecond, false, ""},
		{"without-kernel", 0, false, ""},
		{"compressed-shorter-than-kernel", 5 * time.Second, true, ""},
		{"compressed-longer-than-kernel", 100 * time.Millisecond, true, ""},
		{"compressed-without-kernel", 0, true, ""},
		{"compressed-trailer-stall", 5 * time.Second, true, "stall"},
		{"compressed-trailer-disconnect", 5 * time.Second, true, "disconnect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := uploadTestPool(t, 1, DefaultEndpointLimits().Multipart.Bytes)
			endpoint := admittedEndpoint(t, pool)
			endpoint.route = ConfigureRoute(endpoint.route, Timeout(300*time.Millisecond))
			var publications atomic.Int32
			router, err := NewRouter(endpoint.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				publications.Add(1)
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			var handler stdhttp.Handler = router
			if tc.compression {
				handler, err = ApplyMiddleware(router, Compression(DefaultCompressionConfig()), ETags(DefaultETagConfig()))
				if err != nil {
					t.Fatal(err)
				}
			}
			address, client := uploadDeadlineServer(t, handler, tc.kernel)
			conn, err := net.DialTimeout("tcp", address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, primaryUpload(strings.Repeat("x", 64<<10)))
			started := time.Now()
			if _, err := fmt.Fprintf(conn, "POST /uploads HTTP/1.1\r\nHost: localhost\r\nAccept-Encoding: gzip\r\nContent-Type: %s\r\nTransfer-Encoding: chunked\r\n\r\n", media); err != nil {
				t.Fatal(err)
			}
			stop, sent := make(chan struct{}), make(chan struct{})
			var completed atomic.Bool
			go func() {
				defer close(sent)
				if tc.trailer != "" {
					// The MIME body is complete, but the terminating chunk's trailer
					// section lacks its final blank line. net/http rewrites read errors here.
					if _, err := fmt.Fprintf(conn, "%x\r\n%s\r\n0\r\n", len(wire), wire); err != nil {
						t.Error(err)
						return
					}
					if tc.trailer == "disconnect" {
						if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
							t.Error(err)
						}
					}
					return
				}
				tick := time.NewTicker(25 * time.Millisecond)
				defer tick.Stop()
				for offset := 0; offset < len(wire); {
					select {
					case <-stop:
						return
					case <-tick.C:
					}
					end := min(offset+512, len(wire))
					if _, err := fmt.Fprintf(conn, "%x\r\n%s\r\n", end-offset, wire[offset:end]); err != nil {
						return
					}
					offset = end
				}
				if _, err := io.WriteString(conn, "0\r\n\r\n"); err == nil {
					completed.Store(true)
				}
			}()
			defer func() { close(stop); conn.Close(); <-sent }()
			response, err := stdhttp.ReadResponse(bufio.NewReader(conn), nil)
			if tc.trailer == "disconnect" {
				if err == nil {
					response.Body.Close()
					if response.StatusCode == 408 {
						t.Fatal("trailer disconnect was classified as a read timeout")
					}
				} else if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatal("disconnect did not promptly close the response", err)
				}
			} else {
				if err != nil {
					t.Fatal("upload deadline did not return a response during body progress", err)
				}
				var failure ErrorResponse
				err = json.NewDecoder(response.Body).Decode(&failure)
				response.Body.Close()
				elapsed := time.Since(started)
				if err != nil || response.StatusCode != 408 || failure.Code != RequestTimeout || !response.Close {
					t.Fatal("unfinished upload did not return a closing 408", response.StatusCode, failure, err)
				}
				if elapsed < 200*time.Millisecond || elapsed >= 1500*time.Millisecond || completed.Load() {
					t.Fatal("upload did not enforce its own total deadline", elapsed, completed.Load())
				}
			}
			if publications.Load() != 0 {
				t.Fatal("expired upload entered publication")
			}
			// The response can reach the socket just before the handler's final
			// cleanup. Capacity stays owned until that cleanup actually finishes.
			until := time.Now().Add(time.Second)
			for {
				active, reserved := pool.Usage()
				if active == 0 && reserved == 0 {
					break
				}
				if time.Now().After(until) {
					t.Fatal("expired upload retained admission", active, reserved)
				}
				time.Sleep(time.Millisecond)
			}
			entries, err := os.ReadDir(pool.pool.Directory())
			if err != nil || len(entries) != 1 || entries[0].Name() != ".foundry-upload-owner" {
				t.Fatal("expired upload retained capture files", err)
			}
			healthy, media := multipartWire(t, primaryUpload("healthy"))
			response, err = client.Post("http://"+address+"/uploads", media, bytes.NewReader(healthy))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 204 || publications.Load() != 1 {
				t.Fatal("upload pool did not recover", response.StatusCode, publications.Load())
			}
		})
	}
}

func TestUploadTerminalTimeoutDoesNotMaskDisconnect(t *testing.T) {
	expired := time.Now().Add(-time.Second)
	nativeTimeout := &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
	for _, tc := range []struct {
		name     string
		err      error
		deadline time.Time
		terminal bool
	}{
		{"installed-read-timeout", nativeTimeout, expired, true},
		{"disconnected", io.ErrUnexpectedEOF, expired, false},
		{"canceled", context.Canceled, expired, false},
		{"uninstalled-timeout", nativeTimeout, time.Time{}, false},
		{"earlier-transport-timeout", nativeTimeout, time.Now().Add(time.Minute), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			admitted := &requestUpload{}
			request := httptest.NewRequestWithContext(context.WithValue(ctx, uploadReservationKey{}, admitted), "POST", "/uploads", nil)
			conn := &httpdeadline.Conn{Conn: deadlineErrorConn{reader: iotest.ErrReader(tc.err)}}
			if !tc.deadline.IsZero() {
				admitted.readTimeout, _ = conn.UploadReadDeadline(tc.deadline)
			}
			_, _ = conn.Read(make([]byte, 1))
			observed := admitted.readTimeout != nil && admitted.readTimeout.Observed()
			if observed != tc.terminal {
				t.Fatal("read failure was misclassified", observed)
			}
			if !errors.Is(transportErr(request), context.Canceled) {
				t.Fatal("cancellation was suppressed before selecting the terminal response")
			}
			admitted.timeoutResponse.Store(observed)
			if err := transportErr(request); (err == nil) != tc.terminal {
				t.Fatal("terminal timeout exception masked a disconnect or rejected the timeout", err)
			}
		})
	}
}

type deadlineErrorConn struct {
	net.Conn
	reader io.Reader
}

func (c deadlineErrorConn) Read(p []byte) (int, error)    { return c.reader.Read(p) }
func (deadlineErrorConn) SetReadDeadline(time.Time) error { return nil }
func (deadlineErrorConn) SetDeadline(time.Time) error     { return nil }
