package ratelimit

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"sync"
	"time"
)

// Config bounds declaration count, logical key bytes and entire active operations,
// including key resolvers/codecs. Timeout uses real context deadlines.
type Config struct {
	Namespace                                   keyspace.Namespace
	MaxKeyBytes, MaxDeclarations, MaxConcurrent int
	Timeout                                     time.Duration
}

func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace, MaxKeyBytes: 1024, MaxDeclarations: 256, MaxConcurrent: 128, Timeout: 5 * time.Second}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxKeyBytes <= 0 || c.MaxKeyBytes > keyspace.MaxKeyBytes || c.MaxDeclarations <= 0 || c.MaxConcurrent <= 0 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid rate limit store bounds")
	}
	return nil
}

// Store borrows its Backend. Construction performs no I/O or background work.
// Calls own callbacks until they actually exit, including after cancellation;
// an uncooperative callback retains its slot instead of creating detached work.
type Store struct {
	backend      Backend
	config       Config
	slots        chan struct{}
	mu           sync.Mutex
	declarations map[Name]*declarationID
}

func NewStore(backend Backend, config Config) (*Store, error) {
	if backend == nil {
		return nil, fault.New(fault.Invalid, "rate limit store requires a backend")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, config: config, slots: make(chan struct{}, config.MaxConcurrent), declarations: make(map[Name]*declarationID)}, nil
}
func (s *Store) Namespace() keyspace.Namespace { return s.config.Namespace }
