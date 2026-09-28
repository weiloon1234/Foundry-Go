package raw

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

const (
	MaxArguments     = 256
	MaxCommands      = 256
	MaxRequestBytes  = 16 << 20
	MaxArgumentBytes = 1 << 20
	MaxScriptBytes   = 64 << 10
	MaxReplyBytes    = 16 << 20
	MaxReplyNodes    = 65536
	MaxReplyDepth    = 64
)

// ReplyLimits apply to the complete reply, or the combined batch. They bound
// retained/captured replies after the driver parses RESP, not malicious server
// parser allocations. Depth counts the root as zero.
type ReplyLimits struct{ Bytes, Nodes, Depth int }

func DefaultReplyLimits() ReplyLimits { return ReplyLimits{1 << 20, 8192, 32} }
func (l ReplyLimits) Validate() error {
	if l.Bytes <= 0 || l.Bytes > MaxReplyBytes || l.Nodes <= 0 || l.Nodes > MaxReplyNodes || l.Depth < 0 || l.Depth > MaxReplyDepth {
		return fault.New(fault.Invalid, "invalid raw Redis reply bounds")
	}
	return nil
}

type Limits struct {
	Arguments, Commands, RequestBytes int
	Reply                             ReplyLimits
}

func DefaultLimits() Limits { return Limits{MaxArguments, 64, 1 << 20, DefaultReplyLimits()} }
func (l Limits) Validate() error {
	if l.Arguments <= 0 || l.Arguments > MaxArguments || l.Commands <= 0 || l.Commands > MaxCommands || l.RequestBytes <= 0 || l.RequestBytes > MaxRequestBytes {
		return fault.New(fault.Invalid, "invalid raw Redis request bounds")
	}
	return l.Reply.Validate()
}

type Config struct {
	Namespace                                   keyspace.Namespace
	MaxDeclarations, MaxKeyBytes, MaxConcurrent int
	Timeout                                     time.Duration
	Limits                                      Limits
}

func DefaultConfig(ns keyspace.Namespace) Config {
	return Config{ns, 256, 1024, 128, 5 * time.Second, DefaultLimits()}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxDeclarations <= 0 || c.MaxKeyBytes <= 0 || c.MaxKeyBytes > keyspace.MaxKeyBytes || c.MaxConcurrent <= 0 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid raw Redis store bounds")
	}
	return c.Limits.Validate()
}
