package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	stdhttp "net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeServerBoundsAcceptedConnectionsBeforeHandlerAdmission(t *testing.T) {
	firstEntered, secondEntered, secondConnected, release := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	var handled, dialed atomic.Int32
	config := DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.MaxConnections = 1
	server, err := Prepare(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		if handled.Add(1) == 1 {
			close(firstEntered)
			<-release
		} else {
			close(secondEntered)
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	}), config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		resume()
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("connection-bounded server did not finish")
		}
	})
	ready, readyCancel := context.WithTimeout(t.Context(), 3*time.Second)
	address, err := server.Ready(ready)
	readyCancel()
	if err != nil {
		t.Fatal(err)
	}
	transport := &stdhttp.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err == nil && dialed.Add(1) == 2 {
			close(secondConnected)
		}
		return conn, err
	}}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 3 * time.Second}
	results := make(chan error, 2)
	request := func() {
		response, err := client.Get("http://" + address + "/")
		if response != nil {
			response.Body.Close()
			if response.StatusCode != stdhttp.StatusNoContent {
				t.Error("unexpected response status", response.StatusCode)
			}
		}
		results <- err
	}
	go request()
	select {
	case <-firstEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first connection did not enter")
	}
	go request()
	select {
	case <-secondConnected:
	case <-time.After(3 * time.Second):
		t.Fatal("second peer did not reach the OS backlog")
	}
	select {
	case <-secondEntered:
		t.Fatal("connection limit failed before request admission")
	case <-time.After(30 * time.Millisecond):
	}
	resume()
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("released connection did not admit waiting peer")
		}
	}
	if handled.Load() != 2 {
		t.Fatal("connection budget stranded waiting work")
	}
}
