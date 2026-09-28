// Package upload owns request-scoped temporary files behind typed HTTP handles.
package upload

import "github.com/weiloon1234/Foundry-Go/fault"

const bufferBytes = 32 << 10
const sniffBytes = 512

// Config bounds capture bytes, created files and concurrently open readers.
// Empty TempDirectory uses the operating system's temporary directory.
type Config struct {
	TempDirectory          string
	MaxBytes, MaxFileBytes int64
	MaxFiles, MaxReaders   int
}

func (c Config) Validate() error {
	if c.MaxBytes <= 0 || c.MaxFileBytes <= 0 || c.MaxFileBytes > c.MaxBytes || c.MaxFiles <= 0 || c.MaxReaders <= 0 {
		return fault.New(fault.Invalid, "invalid upload resource limits")
	}
	return nil
}

type Limit uint8

const (
	TotalBytes Limit = iota + 1
	FileBytes
	Files
	Readers
)

type LimitError struct{ Kind Limit }

func (*LimitError) Error() string { return "upload resource limit exceeded" }

// ReadError distinguishes failed incoming bytes from local filesystem failures.
// Error formatting never invokes the underlying reader's error methods.
type ReadError struct{ Cause error }

func (*ReadError) Error() string   { return "uploaded bytes could not be read" }
func (e *ReadError) Unwrap() error { return e.Cause }

// CleanupError reports a failed local cleanup rather than misclassifying an
// incoming reader failure as the complete outcome. The cause remains private.
type CleanupError struct{ Cause error }

func (*CleanupError) Error() string   { return "upload cleanup failed" }
func (e *CleanupError) Unwrap() error { return e.Cause }
