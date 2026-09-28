package logging

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// RotationConfig controls file sinks. A nonempty file rolls over before a write
// on a new day in the sink's timezone or when that write would exceed MaxBytes. Zero limits select
// defaults; Disabled explicitly opts into externally managed append-only files.
// MaxFiles counts archives, excluding the active file. Retention uses archive time.
type RotationConfig struct {
	MaxBytes int64
	MaxFiles int
	MaxAge   time.Duration
	Disabled bool
}

func DefaultRotationConfig() RotationConfig {
	return RotationConfig{MaxBytes: 20 << 20, MaxFiles: 14, MaxAge: 14 * 24 * time.Hour}
}

func (c RotationConfig) resolved() RotationConfig {
	defaults := DefaultRotationConfig()
	if c.MaxBytes == 0 {
		c.MaxBytes = defaults.MaxBytes
	}
	if c.MaxFiles == 0 {
		c.MaxFiles = defaults.MaxFiles
	}
	if c.MaxAge == 0 {
		c.MaxAge = defaults.MaxAge
	}
	return c
}

func (c RotationConfig) Validate() error {
	c = c.resolved()
	if c.MaxBytes < 1 || c.MaxBytes > 1<<30 || c.MaxFiles < 1 || c.MaxFiles > 1000 || c.MaxAge < time.Second || c.MaxAge > 365*24*time.Hour {
		return fault.New(fault.Invalid, "log rotation limits must be bounded and positive")
	}
	return nil
}
