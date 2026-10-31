package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// BrokerDriver selects a pub/sub broker implementation.
type BrokerDriver string

const (
	MemoryBroker BrokerDriver = "memory"
	RedisBroker  BrokerDriver = "redis"
)

// BrokerSettings configures a broker driver, optional Redis connection, and pub/sub behavior.
//
//foundry:config
type BrokerSettings struct {
	Driver BrokerDriver
	Redis  redis.ConnectionName
	Config pubsub.Config
}

// DefaultBrokerSettings returns default settings for an in-memory broker.
func DefaultBrokerSettings() BrokerSettings {
	return BrokerSettings{Driver: MemoryBroker, Config: pubsub.DefaultConfig(keyspace.Namespace{})}
}

// Brokers maps pub/sub connection names to broker settings.
type Brokers map[pubsub.ConnectionName]BrokerSettings

// UnmarshalText decodes broker settings, replacing the receiver only on success.
func (m *Brokers) UnmarshalText(data []byte) error {
	schema, err := BrokerSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(pubsub.ConnectionName) BrokerSettings { return DefaultBrokerSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

// PubSubSettings configures named pub/sub connections and their default.
type PubSubSettings struct {
	Default     pubsub.ConnectionName
	Connections Brokers
}

// DefaultPubSubSettings returns pub/sub settings selecting the default connection name.
func DefaultPubSubSettings() PubSubSettings { return PubSubSettings{Default: "default"} }

// RealtimeDriver selects a realtime connection implementation.
type RealtimeDriver string

const (
	LocalRealtime RealtimeDriver = "local"
	RedisRealtime RealtimeDriver = "redis"
)

// RealtimeConnectionSettings configures a realtime driver, optional Redis connection, and cluster
// behavior.
//
//foundry:config
type RealtimeConnectionSettings struct {
	Driver  RealtimeDriver
	Redis   redis.ConnectionName
	Cluster websocket.ClusterConfig
}

// DefaultRealtimeConnectionSettings returns default settings for a local realtime connection.
func DefaultRealtimeConnectionSettings() RealtimeConnectionSettings {
	return RealtimeConnectionSettings{Driver: LocalRealtime, Cluster: websocket.DefaultClusterConfig(keyspace.Namespace{})}
}

// RealtimeConnections maps realtime connection names to settings.
type RealtimeConnections map[websocket.ConnectionName]RealtimeConnectionSettings

// UnmarshalText decodes realtime connection settings, replacing the receiver only on success.
func (m *RealtimeConnections) UnmarshalText(data []byte) error {
	schema, err := RealtimeConnectionSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(websocket.ConnectionName) RealtimeConnectionSettings { return DefaultRealtimeConnectionSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

// RealtimeSettings configures named realtime connections and their default.
type RealtimeSettings struct {
	Default     websocket.ConnectionName
	Connections RealtimeConnections
}

// DefaultRealtimeSettings returns realtime settings selecting the default connection name.
func DefaultRealtimeSettings() RealtimeSettings { return RealtimeSettings{Default: "default"} }
