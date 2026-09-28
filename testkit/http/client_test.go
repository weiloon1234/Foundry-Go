package http_test

import (
	"context"
	"errors"
	stdhttp "net/http"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
)

func TestClientUsesHandlerAndClosesWithTest(t *testing.T) {
	var retained *httptest.Client
	t.Run("owner", func(t *testing.T) {
		retained = httptest.New(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if r.Header.Get("Authorization") != "test-token" {
				w.WriteHeader(401)
				return
			}
			w.WriteHeader(202)
			_, _ = w.Write([]byte("accepted"))
		}))
		response, err := retained.Do(t.Context(), retained.Get("/private"))
		if err != nil {
			t.Fatal(err)
		}
		httptest.AssertStatus(t, response, 401)
		response, err = retained.Do(t.Context(), retained.Get("/private").Header("Authorization", "test-token"))
		if err != nil {
			t.Fatal(err)
		}
		httptest.AssertStatus(t, response, 202)
		if string(response.Bytes()) != "accepted" {
			t.Fatal("handler response changed")
		}
	})
	select {
	case <-retained.Done():
	default:
		t.Fatal("test cleanup did not close client")
	}
	if _, err := retained.Do(t.Context(), retained.Get("/private")); err == nil {
		t.Fatal("closed test client remained usable")
	}
}

func TestClientRetainsBodyBoundsCancellationAndIsolation(t *testing.T) {
	large := httptest.New(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (4<<20)+1)))
	}))
	if _, err := large.Do(t.Context(), large.Get("/")); err == nil {
		t.Fatal("oversized response accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := large.Do(ctx, large.Get("/")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	for _, body := range []string{"one", "two"} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			client := httptest.New(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { _, _ = w.Write([]byte(body)) }))
			response, err := client.Do(t.Context(), client.Get("/"))
			if err != nil || string(response.Bytes()) != body {
				t.Fatal("test servers share state", err)
			}
		})
	}
	if _, err := httptest.DecodeJSON(t.Context(), httpclient.Response{}, contract.JSON[struct{}]{}); err == nil {
		t.Fatal("empty response accepted")
	}
}
