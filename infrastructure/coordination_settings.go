package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/redis"
)

// CoordinationDriver selects a coordination implementation.
type CoordinationDriver string

const (
	MemoryCoordination CoordinationDriver = "memory"
	RedisCoordination  CoordinationDriver = "redis"
)

// CoordinationSettings configures coordination enablement, backend, leases, and capacity.
type CoordinationSettings struct {
	Enabled    bool
	Driver     CoordinationDriver
	Redis      redis.ConnectionName
	Config     lease.Config
	MaxEntries int
}

// DefaultCoordinationSettings returns default settings for in-memory coordination.
func DefaultCoordinationSettings() CoordinationSettings {
	return CoordinationSettings{Driver: MemoryCoordination, Config: lease.DefaultConfig(keyspace.Namespace{}), MaxEntries: 4096}
}
