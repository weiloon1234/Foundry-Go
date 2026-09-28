package redis

import (
	"github.com/weiloon1234/Foundry-Go/internal/websockettest"
	"testing"
)

func TestRedisWebSocketDistributedRuntime(t *testing.T) {
	client, namespace, _ := integrationAddresses(t, nil)
	backend, err := NewWebSocketBackend(client)
	if err != nil {
		t.Fatal(err)
	}
	websockettest.Run(t, backend, namespace)
}
