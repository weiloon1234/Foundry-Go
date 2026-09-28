package http

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestStreamNativeConnectionCannotReportTruncationAsSuccess(t *testing.T) {
	for _, mode := range []string{"short", "long", "unknown-error"} {
		t.Run(mode, func(t *testing.T) {
			closed := make(chan struct{})
			router := streamRouter(t, streamEndpoint(), func(context.Context) (StreamContent, error) {
				size := 65536
				if mode == "long" {
					size++
				}
				source := strings.NewReader(strings.Repeat("x", size))
				read := func(p []byte) (int, error) {
					n, err := source.Read(p)
					if mode == "unknown-error" && err == io.EOF {
						return n, errors.New("private-producer-error")
					}
					return n, err
				}
				c := StreamContent{Body: fileReaderCallbacks{read: read, close: func() error { close(closed); return nil }}, MediaType: "text/plain; charset=utf-8"}
				if mode == "short" {
					c.Length = value.Set(int64(size + 1))
				}
				if mode == "long" {
					c.Length = value.Set(int64(size - 1))
				}
				return c, nil
			})
			server := httptest.NewServer(router)
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			response, err := client.Get(server.URL + "/stream")
			if err == nil {
				_, err = io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
			if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
				t.Fatal("truncated representation reported success", err)
			}
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("source leaked")
			}
		})
	}
}

func TestStreamClientCancellationClosesAfterActiveReadReturns(t *testing.T) {
	entered, closed := make(chan struct{}), make(chan struct{})
	router := streamRouter(t, streamEndpoint(), func(ctx context.Context) (StreamContent, error) {
		reads := 0
		return StreamContent{Body: fileReaderCallbacks{read: func(p []byte) (int, error) {
			reads++
			if reads == 1 {
				clear(p)
				return len(p), nil
			}
			close(entered)
			<-ctx.Done()
			return 0, ctx.Err()
		}, close: func() error { close(closed); return nil }}, MediaType: "text/plain; charset=utf-8"}, nil
	})
	server := httptest.NewServer(router)
	defer server.Close()
	defer server.CloseClientConnections()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, err := stdhttp.NewRequestWithContext(ctx, "GET", server.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := io.ReadFull(response.Body, make([]byte, 1)); err != nil {
		t.Fatal("response buffered whole stream", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not start")
	}
	cancel()
	response.Body.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled read was abandoned")
	}
}
