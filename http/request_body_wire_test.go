package http

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"
)

func startBoundaryServer(t *testing.T, handler stdhttp.Handler) string {
	t.Helper()
	config := DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.MaxBodyBytes = 8
	server, err := Prepare(handler, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("boundary server shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("boundary server did not finish")
		}
	})
	ready, stopReady := context.WithTimeout(t.Context(), 2*time.Second)
	defer stopReady()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func TestOversizedContinueRequestRejectsWithoutWaitingForBody(t *testing.T) {
	address := startBoundaryServer(t, stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {
		t.Error("oversized declaration reached native handler")
	}))
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, err = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\nExpect: 100-continue\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	response, err := stdhttp.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("rejection waited for a body that was not sent: %v", err)
	}
	defer response.Body.Close()
	var failure ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 413 || failure.Code != PayloadTooLarge || failure.Status != 413 || !response.Close {
		t.Fatalf("declared oversized stream response: %#v", failure)
	}
	if response.Header.Get(RequestIDHeader) == "" || response.Header.Get(RequestIDHeader) != string(failure.RequestID) {
		t.Fatal("early wire rejection lacked consistent request identity")
	}
}

func TestChunkedBodyLimitUsesSharedErrorResponse(t *testing.T) {
	address := startBoundaryServer(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, err := io.ReadAll(r.Body)
		var tooLarge *stdhttp.MaxBytesError
		if !errors.As(err, &tooLarge) {
			t.Errorf("chunked body lacked typed limit error: %v", err)
			return
		}
		if err := WriteError(w, r, PayloadTooLarge.WithCause(err)); err != nil {
			t.Error(err)
		}
	}))
	transport := &stdhttp.Transport{}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 2 * time.Second}
	request, err := stdhttp.NewRequestWithContext(t.Context(), stdhttp.MethodPost, "http://"+address, io.NopCloser(strings.NewReader(strings.Repeat("x", 100))))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var failure ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 413 || failure.Code != PayloadTooLarge || string(failure.RequestID) != response.Header.Get(RequestIDHeader) {
		t.Fatalf("chunked rejection: %#v", failure)
	}
}
