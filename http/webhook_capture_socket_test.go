package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
)

func captureSocketServer(t *testing.T, handler stdhttp.Handler, recorder *httpcapture.Recorder[httpcapture.Snapshot], readTimeout time.Duration) string {
	t.Helper()
	config := DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.ReadTimeout = readTimeout
	prefix, _ := ParsePathPrefix("/wh")
	server, err := Prepare(handler, config, slog.New(slog.NewTextHandler(io.Discard, nil)), WithWebhookCapture(recorder, CapturePrefix(prefix)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	ready, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func TestWebhookCaptureNativeReadFailure(t *testing.T) {
	for _, mode := range []string{"disconnect", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			recorder := captureTestRecorder(t)
			address := captureSocketServer(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { _, _ = io.ReadAll(r.Body); w.WriteHeader(204) }), recorder, 80*time.Millisecond)
			conn, err := net.Dial("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			_, err = io.WriteString(conn, "POST /wh HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\nConnection: close\r\n\r\npart")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "disconnect" {
				if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
					t.Fatal(err)
				}
			}
			_, _ = io.ReadAll(conn)
			select {
			case <-recorder.Ready():
			case <-time.After(2 * time.Second):
				t.Fatal("read failure did not complete")
			}
			s := captureTestTake(t, recorder)
			want := httpcapture.ClientDisconnect
			if mode == "timeout" {
				want = httpcapture.ReadTimeout
			}
			if s.Body.ReadFailure != want || s.Body.End != httpcapture.SourceFailure || string(s.Body.Prefix) != "part" || s.Body.ObservedBytes != 4 || !s.Body.MeasuredBytes.IsNull() {
				t.Fatal("native read failure snapshot incorrect")
			}
		})
	}
}

func TestWebhookCaptureRawPanicAbortsNativeConnection(t *testing.T) {
	recorder := captureTestRecorder(t)
	address := captureSocketServer(t, stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) { panic("private") }), recorder, time.Second)
	client := &stdhttp.Client{Timeout: 2 * time.Second}
	response, err := client.Post("http://"+address+"/wh", "text/plain", strings.NewReader("body"))
	if response != nil {
		_ = response.Body.Close()
		t.Fatal("raw panic fabricated an HTTP response")
	}
	if err == nil {
		t.Fatal("raw panic did not abort")
	}
	select {
	case <-recorder.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("abort was not captured")
	}
	s := captureTestTake(t, recorder)
	if !s.Completion.Aborted || !s.Completion.Status.IsNull() || string(s.Body.Prefix) != "body" {
		t.Fatal("aborted capture missing")
	}
}

func TestWebhookCaptureNativeInformationalAndHEAD(t *testing.T) {
	recorder := captureTestRecorder(t)
	address := captureSocketServer(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(103)
		w.WriteHeader(102)
		w.WriteHeader(201)
		_, _ = io.WriteString(w, "entity")
	}), recorder, time.Second)
	client := &stdhttp.Client{Timeout: 2 * time.Second}
	for _, method := range []string{"GET", "HEAD"} {
		r, _ := stdhttp.NewRequest(method, "http://"+address+"/wh", nil)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != 201 {
			t.Fatal("informational response changed final status", err)
		}
		select {
		case <-recorder.Ready():
		case <-time.After(2 * time.Second):
			t.Fatal("response not captured")
		}
		s := captureTestTake(t, recorder)
		if status, ok := s.Completion.Status.Get(); !ok || status != 201 {
			t.Fatal("first informational status was treated as final")
		}
		if method == "HEAD" {
			if len(body) != 0 || len(s.Completion.ResponseHead) != 0 {
				t.Fatal("HEAD retained an unsent entity")
			}
		} else if string(body) != "entity" || string(s.Completion.ResponseHead) != "entity" {
			t.Fatal("native response head mismatch")
		}
	}
	client.CloseIdleConnections()
}
