package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type websocketDiagnosticReply struct{ status int64 }

func (h websocketDiagnosticReply) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h websocketDiagnosticReply) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h websocketDiagnosticReply) ProcessHook(driver.ProcessHook) driver.ProcessHook {
	return func(_ context.Context, command driver.Cmder) error {
		command.(*driver.Cmd).SetVal([]any{h.status})
		return nil
	}
}

func TestWebSocketNativeStatusDiagnosticsAcrossContainment(t *testing.T) {
	limits := websocket.ClusterLimits{Connections: 8, ConnectionsPerSubject: 2, Subscriptions: 4, PresenceMembers: 4, MemberBytes: 64, FrameBytes: 8192, ConnectionTTL: time.Second, Retention: 2 * time.Second}
	key, err := websocket.NewClusterKey(keyspace.Namespace{Application: "diagnostic", Environment: "test"}, strings.Repeat("a", 64), limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		status int64
		code   websocket.Code
	}{{-3, websocket.CapacityExceeded}, {-4, websocket.Stopping}} {
		t.Run(string(test.code), func(t *testing.T) {
			config := DefaultConfig()
			config.Host, config.TLS = "localhost", DisableTLS
			client, err := Prepare(config)
			if err != nil {
				t.Fatal(err)
			}
			client.raw = driver.NewClient(&driver.Options{Addr: "localhost:6379", MaxRetries: -1})
			client.raw.AddHook(websocketDiagnosticReply{status: test.status})
			client.ready.Store(true)
			defer client.Close(context.Background())
			backend, err := NewWebSocketBackend(client)
			if err != nil {
				t.Fatal(err)
			}
			source := backend.WebSocketCheck(t.Context(), key)
			got := callback.Isolated("consumer", func() error { return source })
			if source == nil || source.Error() != string(test.code) || got.Error() != "consumer: "+string(test.code) || !errors.Is(got, source) || !errors.Is(got, test.code) {
				t.Fatal("Redis WebSocket status diagnostic or identity was lost")
			}
		})
	}
}
