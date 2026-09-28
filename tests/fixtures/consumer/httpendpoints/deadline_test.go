package httpendpoints_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/httpdto"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestServerDeadlineReachesTypedDomainHandler(t *testing.T) {
	t.Parallel()
	var canceled atomic.Bool
	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "slow", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, httpdto.UserResponseJSON()),
	)
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (httpdto.UserResponse, error) {
		<-ctx.Done()
		canceled.Store(ctx.Err() == context.DeadlineExceeded)
		return httpdto.UserResponse{}, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	config.RequestTimeout = 50 * time.Millisecond
	server, err := foundryhttp.Prepare(router, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("deadline server did not finish shutdown")
		}
	})
	ready, cancelReady := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelReady()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	transport := &stdhttp.Transport{}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 3 * time.Second}
	request, err := stdhttp.NewRequestWithContext(t.Context(), "GET", "http://"+address+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var failure foundryhttp.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 408 || failure.Code != foundryhttp.RequestTimeout || !canceled.Load() {
		t.Fatalf("typed deadline response: %d %+v", response.StatusCode, failure)
	}
	if failure.RequestID == "" || string(failure.RequestID) != response.Header.Get(foundryhttp.RequestIDHeader) {
		t.Fatal("deadline response lost request correlation")
	}
}
