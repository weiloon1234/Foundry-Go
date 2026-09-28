package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type BrokerDriver string

const (
	MemoryBroker BrokerDriver = "memory"
	RedisBroker  BrokerDriver = "redis"
)

//foundry:config
type BrokerSettings struct {
	Driver BrokerDriver
	Redis  redis.ConnectionName
	Config pubsub.Config
}

func DefaultBrokerSettings() BrokerSettings {
	return BrokerSettings{Driver: MemoryBroker, Config: pubsub.DefaultConfig(keyspace.Namespace{})}
}

type Brokers map[pubsub.ConnectionName]BrokerSettings

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

type PubSubSettings struct {
	Default     pubsub.ConnectionName
	Connections Brokers
}

func DefaultPubSubSettings() PubSubSettings { return PubSubSettings{Default: "default"} }

type RealtimeDriver string

const (
	LocalRealtime RealtimeDriver = "local"
	RedisRealtime RealtimeDriver = "redis"
)

//foundry:config
type RealtimeConnectionSettings struct {
	Driver  RealtimeDriver
	Redis   redis.ConnectionName
	Cluster websocket.ClusterConfig
}

func DefaultRealtimeConnectionSettings() RealtimeConnectionSettings {
	return RealtimeConnectionSettings{Driver: LocalRealtime, Cluster: websocket.DefaultClusterConfig(keyspace.Namespace{})}
}

type RealtimeConnections map[websocket.ConnectionName]RealtimeConnectionSettings

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

type RealtimeSettings struct {
	Default     websocket.ConnectionName
	Connections RealtimeConnections
}

func DefaultRealtimeSettings() RealtimeSettings { return RealtimeSettings{Default: "default"} }
