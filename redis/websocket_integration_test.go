package redis

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/websockettest"
	"github.com/weiloon1234/Foundry-Go/websocket"
	"testing"
)

func TestRedisWebSocketDistributedRuntime(t *testing.T) {
	client, namespace, track := integrationAddresses(t, nil, websocket.KeyFeature)
	backend, err := NewWebSocketBackend(client)
	if err != nil {
		t.Fatal(err)
	}
	websockettest.Run(t, trackedWebSocketBackend{ClusterBackend: backend, track: track}, namespace)
}

// Policy checks can create metadata even when no connection is admitted.
type trackedWebSocketBackend struct {
	websocket.ClusterBackend
	track func(string)
}

func (b trackedWebSocketBackend) WebSocketCheck(ctx context.Context, key websocket.ClusterKey) error {
	b.track(key.String())
	return b.ClusterBackend.WebSocketCheck(ctx, key)
}
