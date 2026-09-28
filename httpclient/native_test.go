package httpclient_test

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/httpclient"
)

func TestNativeTransportRetriesReusesConnectionsAndDoesNotFollowRedirects(t *testing.T) {
	var calls, connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/v1/retry":
			if calls.Add(1) < 3 {
				w.WriteHeader(503)
				_, _ = w.Write([]byte("retry"))
				return
			}
			_, _ = w.Write([]byte("ok"))
		case "/v1/redirect":
			w.Header().Set("Location", "/must-not-follow")
			w.WriteHeader(302)
		case "/v1/mutation":
			calls.Add(1)
			w.WriteHeader(503)
		default:
			t.Error("unexpected redirect or changed base path")
			w.WriteHeader(500)
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	config := testConfig()
	config.BaseURL = server.URL + "/v1"
	c := newClient(t, config, nil)
	response, err := c.Do(t.Context(), c.Get("retry"))
	if err != nil || response.Attempts() != 3 || response.Status() != 200 {
		t.Fatal(response, err)
	}
	if connections.Load() != 1 {
		t.Fatal("bounded retry drain lost native connection reuse", connections.Load())
	}
	response, err = c.Do(t.Context(), c.Get("redirect"))
	if err != nil || response.Status() != 302 {
		t.Fatal(response, err)
	}
	response, err = c.Do(t.Context(), c.Post("mutation").Header("Idempotency-Key", "one-call"))
	if err != nil || response.Attempts() != 1 || response.Status() != 503 || calls.Load() != 4 {
		t.Fatal("empty POST retried implicitly", response, err, calls.Load())
	}
}

func TestNativeDeadlinesAndDecompressedResponseBound(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		_, _ = writer.Write([]byte(strings.Repeat("x", 4096)))
		_ = writer.Close()
	}))
	t.Cleanup(server.Close)
	config := testConfig()
	config.BaseURL = server.URL
	config.Retry = httpclient.NoRetries()
	config.AttemptTimeout = 30 * time.Millisecond
	config.ResponseBytes = 64
	c := newClient(t, config, nil)
	result := make(chan error, 1)
	go func() { _, err := c.Do(context.Background(), c.Get("slow")); result <- err }()
	await(t, started)
	if err := awaitResult(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline identity lost", err)
	}
	await(t, canceled)
	if response, err := c.Do(t.Context(), c.Get("compressed")); err == nil || len(response.Bytes()) != 0 {
		t.Fatal("decompressed body escaped limit", err)
	}
}

func TestDeclaredBodyLengthMismatchAndMalformedResponsesCloseResources(t *testing.T) {
	for _, test := range []struct {
		name   string
		length int64
		body   string
	}{{"short", 8, "short"}, {"long", 1, "long"}, {"invalid", -2, "body"}} {
		t.Run(test.name, func(t *testing.T) {
			var closed atomic.Int32
			c := newClient(t, testConfig(), roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: test.length, Body: &countBody{Reader: strings.NewReader(test.body), closed: &closed}}, nil
			}))
			if response, err := c.Do(t.Context(), c.Get("invalid")); err == nil || len(response.Bytes()) != 0 || closed.Load() != 1 {
				t.Fatal("malformed response not rejected and closed", err, closed.Load())
			}
		})
	}
	for _, length := range []int64{1, 10} {
		t.Run("request", func(t *testing.T) {
			c := newClient(t, testConfig(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
				_, _ = io.ReadAll(r.Body)
				return &http.Response{StatusCode: 204, Body: http.NoBody, ContentLength: 0}, nil
			}))
			body := httpclient.StreamBody(length, func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("five!")), nil })
			if _, err := c.Do(t.Context(), c.Post("invalid").WithBody(body)); err == nil {
				t.Fatal("swallowed upload length mismatch succeeded")
			}
		})
	}
}
