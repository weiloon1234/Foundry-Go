package websocket

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

// ClusterConfig distinguishes authority-wide quotas from Config's local limits.
// Transient authority failures fail only the affected operation. A lost fan-out
// stream is resubscribed with backoff; no mutation retry is inferred from the
// Redis client's ability to reconnect. Only a policy conflict terminates a Hub.
type ClusterConfig struct {
	Namespace                keyspace.Namespace
	MaxConnections           int
	MaxConnectionsPerSubject int
	ConnectionTTL            time.Duration
	OperationTimeout         time.Duration
	Buffer                   pubsub.Limits
	// RetainConnectionsOnGap keeps local sockets open across a lost fan-out
	// stream; events published during the gap are then missed silently. The
	// default closes them with a retryable status so clients reconnect and
	// request replay.
	RetainConnectionsOnGap bool
	// ExcludeRemoteConnections lets ExceptConnection name a connection hosted
	// by another instance by carrying the exclusion in the fan-out envelope.
	// Instances before this field reject such envelopes, so enable it only
	// once every instance in the namespace runs a release that has it.
	// Excluding a connection of this instance (RelayToOthers) never needs it.
	ExcludeRemoteConnections bool
}

func DefaultClusterConfig(namespace keyspace.Namespace) ClusterConfig {
	return ClusterConfig{Namespace: namespace, MaxConnections: 65536, MaxConnectionsPerSubject: 8, ConnectionTTL: 30 * time.Second, OperationTimeout: 5 * time.Second, Buffer: pubsub.Limits{Messages: 4096, Bytes: 32 << 20, PayloadBytes: 128 << 10, Channels: 1}}
}
func (c ClusterConfig) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxConnections < 1 || c.MaxConnections > 1<<20 || c.MaxConnectionsPerSubject < 1 || c.MaxConnectionsPerSubject > c.MaxConnections || c.ConnectionTTL < 100*time.Millisecond || c.ConnectionTTL > 5*time.Minute || c.ConnectionTTL%time.Millisecond != 0 || c.OperationTimeout < time.Millisecond || c.OperationTimeout > c.ConnectionTTL/2 {
		return fault.New(fault.Invalid, "invalid WebSocket distributed bounds")
	}
	return c.Buffer.Validate()
}
