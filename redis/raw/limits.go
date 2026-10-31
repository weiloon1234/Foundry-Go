package raw

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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

// DefaultReplyLimits returns reply bounds of 1 MiB, 8,192 nodes, and depth 32.
func DefaultReplyLimits() ReplyLimits { return ReplyLimits{1 << 20, 8192, 32} }

// Validate checks byte, node, and depth bounds against the package maxima.
func (l ReplyLimits) Validate() error {
	if l.Bytes <= 0 || l.Bytes > MaxReplyBytes || l.Nodes <= 0 || l.Nodes > MaxReplyNodes || l.Depth < 0 || l.Depth > MaxReplyDepth {
		return safeerror.NewFault(fault.Invalid, "invalid raw Redis reply bounds")
	}
	return nil
}

// Limits bounds request arguments, batch commands, request bytes, and captured replies.
type Limits struct {
	Arguments, Commands, RequestBytes int
	Reply                             ReplyLimits
}

// DefaultLimits returns bounded request and reply defaults with at most 64 batch commands.
func DefaultLimits() Limits { return Limits{MaxArguments, 64, 1 << 20, DefaultReplyLimits()} }

// Validate checks request bounds and the nested reply limits.
func (l Limits) Validate() error {
	if l.Arguments <= 0 || l.Arguments > MaxArguments || l.Commands <= 0 || l.Commands > MaxCommands || l.RequestBytes <= 0 || l.RequestBytes > MaxRequestBytes {
		return safeerror.NewFault(fault.Invalid, "invalid raw Redis request bounds")
	}
	return l.Reply.Validate()
}

// Config sets the raw store namespace, declaration capacity, concurrency, timeout, and request limits.
type Config struct {
	Namespace                                   keyspace.Namespace
	MaxDeclarations, MaxKeyBytes, MaxConcurrent int
	Timeout                                     time.Duration
	Limits                                      Limits
}

// DefaultConfig returns bounded raw store defaults for the supplied namespace.
func DefaultConfig(ns keyspace.Namespace) Config {
	return Config{ns, 256, 1024, 128, 5 * time.Second, DefaultLimits()}
}

// Validate checks the namespace, store bounds, timeout, and request limits.
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxDeclarations <= 0 || c.MaxKeyBytes <= 0 || c.MaxKeyBytes > keyspace.MaxKeyBytes || c.MaxConcurrent <= 0 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return safeerror.NewFault(fault.Invalid, "invalid raw Redis store bounds")
	}
	return c.Limits.Validate()
}
