package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/redis"
)

type CoordinationDriver string

const (
	MemoryCoordination CoordinationDriver = "memory"
	RedisCoordination  CoordinationDriver = "redis"
)

type CoordinationSettings struct {
	Enabled    bool
	Driver     CoordinationDriver
	Redis      redis.ConnectionName
	Config     lease.Config
	MaxEntries int
}

func DefaultCoordinationSettings() CoordinationSettings {
	return CoordinationSettings{Driver: MemoryCoordination, Config: lease.DefaultConfig(keyspace.Namespace{}), MaxEntries: 4096}
}
