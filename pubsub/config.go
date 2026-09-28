package pubsub

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Config bounds publication/setup callbacks and separately bounds subscriptions,
// each of which permits one active receive/decode operation at a time.
type Config struct {
	Namespace                                                     keyspace.Namespace
	MaxDeclarations, MaxKeyBytes, MaxConcurrent, MaxSubscriptions int
	Timeout                                                       time.Duration
	Buffer                                                        Limits
}

func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace, MaxDeclarations: 256, MaxKeyBytes: 1024, MaxConcurrent: 128, MaxSubscriptions: 64, Timeout: 5 * time.Second, Buffer: DefaultLimits()}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxDeclarations <= 0 || c.MaxKeyBytes <= 0 || c.MaxKeyBytes > keyspace.MaxKeyBytes || c.MaxConcurrent <= 0 || c.MaxSubscriptions <= 0 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid pub/sub broker bounds")
	}
	return c.Buffer.Validate()
}
