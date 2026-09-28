// Package password provides bounded Argon2id hashing and verification. Hashes
// and plaintext remain distinct redacted values. Login eligibility, throttling,
// MFA and credential issuance belong to the model-first authentication flow.
package password

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"time"
)

const (
	MaxPasswordBytes        = 4096
	MaxEncodedBytes         = 512
	MaxMemoryKiB     uint32 = 1024 * 1024
	MaxIterations    uint32 = 16
	MaxParallelism   uint8  = 16
	SaltBytes               = 16
	KeyBytes                = 32
)

// Parameters measures memory in KiB and iterations in passes. These values
// affect resource use, not password validation rules. No implicit normalization
// or truncation is performed. ParseHash accepts bounded older costs; new hashing
// requires at least 19 MiB and two passes, with 64 MiB/three passes the default.
type Parameters struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

func (p Parameters) Validate() error {
	if p.Parallelism < 1 || p.Parallelism > MaxParallelism || p.Iterations < 1 || p.Iterations > MaxIterations || p.MemoryKiB < 8*uint32(p.Parallelism) || p.MemoryKiB > MaxMemoryKiB {
		return fault.New(fault.Invalid, "password hash parameters exceed supported bounds")
	}
	return nil
}
func (p Parameters) within(limit Parameters) bool {
	return p.MemoryKiB <= limit.MemoryKiB && p.Iterations <= limit.Iterations && p.Parallelism <= limit.Parallelism
}

// Config sets issuance costs and separate verification ceilings for existing
// hashes. At most MaxConcurrent KDFs run, each bounded by VerifyLimit.MemoryKiB.
// Timeout cancels admission/publication; a started KDF owns its slot until exit
// because Argon2 has no cooperative cancellation. No background hash is abandoned.
type Config struct {
	Parameters    Parameters
	VerifyLimit   Parameters
	MaxConcurrent int
	Timeout       time.Duration
}

func DefaultConfig() Config {
	return Config{Parameters: Parameters{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 4},
		VerifyLimit:   Parameters{MemoryKiB: 128 * 1024, Iterations: 6, Parallelism: 8},
		MaxConcurrent: 2, Timeout: 5 * time.Second}
}
func (c Config) Validate() error {
	if err := c.Parameters.Validate(); err != nil {
		return err
	}
	if err := c.VerifyLimit.Validate(); err != nil {
		return err
	}
	if c.Parameters.MemoryKiB < 19*1024 || c.Parameters.Iterations < 2 || !c.Parameters.within(c.VerifyLimit) || c.MaxConcurrent < 1 || c.MaxConcurrent > 64 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid password hashing policy or capacity")
	}
	return nil
}
