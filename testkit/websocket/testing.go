package websocket

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	protocol "github.com/weiloon1234/Foundry-Go/websocket"
)

// Connect performs the real protocol handshake and owns test cleanup, including
// cleanup after a subsequent Fatal/Goexit. Each test gets its own connection.
func Connect(t testing.TB, url string, headers http.Header, maxFrameBytes int) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, url, headers, maxFrameBytes)
	if err != nil {
		t.Fatalf("connect WebSocket test client: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close WebSocket test client: %v", err)
		}
	})
	return client
}

func AssertType(t testing.TB, response protocol.Response, want protocol.ResponseType) {
	t.Helper()
	if response.Type != want {
		t.Errorf("WebSocket response type: got %s, want %s", response.Type, want)
	}
}

// DecodePayload validates an event payload through its generated contract. The
// caller supplies the exact bounded response received from Client.Receive.
func DecodePayload[T any](ctx context.Context, response protocol.Response, descriptor contract.JSON[T], limits contract.JSONLimits) (T, error) {
	if response.Version != protocol.ProtocolVersion || response.Type != protocol.EventResponse {
		return *new(T), fault.New(fault.Invalid, "expected a WebSocket event response")
	}
	return descriptor.Decode(ctx, response.Payload, limits)
}
