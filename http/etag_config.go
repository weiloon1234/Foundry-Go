package http

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
)

// ETagConfig bounds automatic response hashing. Captures larger than MaxBytes
// pass through in order without an automatic validator. Captures up to one
// 32 KiB page, and complete bodies written once with their declared length,
// need no shared slot. At most MaxConcurrent larger captures are buffered per
// assembled middleware; another waits up to AdmissionWait for a slot and then
// passes through without a validator. Worst-case large-capture memory is
// MaxConcurrent*MaxBytes. Buffering reserves memory only as bytes arrive.
type ETagConfig struct {
	MaxBytes      int64
	MaxConcurrent int
	// AdmissionWait bounds the wait for a large-capture slot, from zero (no
	// wait) through five seconds. The request context also ends the wait.
	AdmissionWait time.Duration
}

func DefaultETagConfig() ETagConfig {
	return ETagConfig{MaxBytes: 10 << 20, MaxConcurrent: 64, AdmissionWait: 100 * time.Millisecond}
}

func (c ETagConfig) Validate() error {
	if c.MaxBytes < 1 || c.MaxBytes > 64<<20 || c.MaxConcurrent < 1 || c.MaxConcurrent > 1024 || c.AdmissionWait < 0 || c.AdmissionWait > admission.DefaultWait {
		return fault.New(fault.Invalid, "automatic ETag capture bounds are invalid")
	}
	return nil
}
