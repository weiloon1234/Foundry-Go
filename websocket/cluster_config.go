package websocket

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// ClusterConfig distinguishes authority-wide quotas from Config's local limits.
// A pub/sub gap terminates this Hub. Restart explicitly; no live continuity or
// mutation retry is inferred from the Redis client's ability to reconnect.
type ClusterConfig struct {
	Namespace                keyspace.Namespace
	MaxConnections           int
	MaxConnectionsPerSubject int
	ConnectionTTL            time.Duration
	OperationTimeout         time.Duration
	Buffer                   pubsub.Limits
}

func DefaultClusterConfig(namespace keyspace.Namespace) ClusterConfig {
	return ClusterConfig{Namespace: namespace, MaxConnections: 1024, MaxConnectionsPerSubject: 8, ConnectionTTL: 30 * time.Second, OperationTimeout: 5 * time.Second, Buffer: pubsub.Limits{Messages: 64, Bytes: 4 << 20, PayloadBytes: 128 << 10, Channels: 1}}
}
func (c ClusterConfig) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxConnections < 1 || c.MaxConnections > 4096 || c.MaxConnectionsPerSubject < 1 || c.MaxConnectionsPerSubject > c.MaxConnections || c.ConnectionTTL < 100*time.Millisecond || c.ConnectionTTL > 5*time.Minute || c.ConnectionTTL%time.Millisecond != 0 || c.OperationTimeout < time.Millisecond || c.OperationTimeout > c.ConnectionTTL/2 {
		return fault.New(fault.Invalid, "invalid WebSocket distributed bounds")
	}
	return c.Buffer.Validate()
}
