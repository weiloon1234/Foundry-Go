package httpkernel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestRequestsShareAttributionAndTypedErrorContracts(t *testing.T) {
	captured := make(chan attribution.RequestID, 1)
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	app, err := foundry.New(foundation.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(
		foundryhttp.Module("http", serverKey, config, func(foundation.Resolver) (stdhttp.Handler, error) {
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				var requestID attribution.RequestID = foundryhttp.RequestID(r.Context())
				if requestID != attribution.FromContext(r.Context()).Request().ID {
					t.Error("HTTP introduced a separate request identity")
				}
				captured <- requestID
				cause := errors.New("private-database-diagnostic")
				if err := foundryhttp.WriteError(w, r, foundryhttp.NotFound.WithCause(cause)); err != nil {
					t.Error(err)
				}
			}), nil
		}),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- app.Run(ctx, foundation.HTTP) }()
	t.Cleanup(func() { cancel(); await(t, finished); await(t, app.Done()) })
	server, err := foundation.Resolve(app.Services(), serverKey)
	if err != nil {
		t.Fatal(err)
	}
	ready, stopReady := context.WithTimeout(t.Context(), 2*time.Second)
	defer stopReady()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	transport := &stdhttp.Transport{}
	defer transport.CloseIdleConnections()
	client := &stdhttp.Client{Transport: transport, Timeout: 2 * time.Second}
	request, err := stdhttp.NewRequestWithContext(t.Context(), stdhttp.MethodGet, "http://"+address, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(foundryhttp.RequestIDHeader, "client-supplied")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var failure foundryhttp.ErrorResponse
	if err := json.Unmarshal(body, &failure); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 404 || failure.Code != foundryhttp.NotFound || strings.Contains(string(body), "private-database-diagnostic") {
		t.Fatalf("public error response: %s", body)
	}
	requestID := await(t, captured)
	if requestID == "" || requestID == "client-supplied" || failure.RequestID != requestID || string(requestID) != response.Header.Get(foundryhttp.RequestIDHeader) {
		t.Fatal("wire response did not reuse generated request attribution")
	}
}
