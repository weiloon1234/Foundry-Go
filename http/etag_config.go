package http

import "github.com/weiloon1234/Foundry-Go/fault"

// ETagConfig bounds automatic response hashing. Captures larger than MaxBytes
// pass through in order without an automatic validator. At most MaxConcurrent
// captures are active per assembled middleware; saturation also passes through.
// Buffering does not reserve MaxBytes until response bytes actually arrive.
type ETagConfig struct {
	MaxBytes      int64
	MaxConcurrent int
}

func DefaultETagConfig() ETagConfig {
	return ETagConfig{MaxBytes: 10 << 20, MaxConcurrent: 4}
}

func (c ETagConfig) Validate() error {
	if c.MaxBytes < 1 || c.MaxBytes > 64<<20 || c.MaxConcurrent < 1 || c.MaxConcurrent > 1024 {
		return fault.New(fault.Invalid, "automatic ETag capture bounds are invalid")
	}
	return nil
}
