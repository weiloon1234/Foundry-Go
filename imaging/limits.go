package imaging

import "time"

// Limits apply before pixel decoding, before each transform, and to output.
// WorkingBytes bounds the conservative admitted workspace (encoded input,
// output allowance and pixel/intermediate buffers). It is not a process heap or
// OS memory quota; codec/runtime bookkeeping and garbage collection are separate.
// There is deliberately no unbounded input option.
type Limits struct {
	InputBytes, OutputBytes int64
	Width, Height           int
	Pixels                  int64
	Frames                  int
	WorkingBytes            int64
}

func DefaultLimits() Limits {
	return Limits{InputBytes: 50 << 20, OutputBytes: 50 << 20, Width: 12000, Height: 12000, Pixels: 25000000, Frames: 64, WorkingBytes: 512 << 20}
}
func (l Limits) Validate() error {
	if l.InputBytes < 1 || l.InputBytes > 1<<30 || l.OutputBytes < 1 || l.OutputBytes > 1<<30 || l.Width < 1 || l.Width > 65535 || l.Height < 1 || l.Height > 65535 || l.Pixels < 1 || l.Pixels > 1<<30 || l.Frames < 1 || l.Frames > 1024 || l.WorkingBytes < 1 || l.WorkingBytes > 16<<30 {
		return invalid("invalid image limits")
	}
	return nil
}
func (l Limits) dimensions(w, h int) error {
	if w < 1 || h < 1 || w > l.Width || h > l.Height || int64(w)*int64(h) > l.Pixels {
		return limited()
	}
	return nil
}
func (l Limits) workspace(input int64, pixels int64) error {
	// Account conservatively for 16-bit decode, conversion, resampling scratch,
	// encoder working copies, and owned output. Arithmetic is bounded by Validate.
	if input*4+pixels*64+l.OutputBytes > l.WorkingBytes {
		return limited()
	}
	return nil
}

type Config struct {
	Limits    Limits
	MaxActive int
	Timeout   time.Duration
}

func DefaultConfig() Config {
	return Config{Limits: DefaultLimits(), MaxActive: 2, Timeout: time.Minute}
}
func (c Config) Validate() error {
	if err := c.Limits.Validate(); err != nil {
		return err
	}
	if c.MaxActive < 1 || c.MaxActive > 64 || c.Timeout <= 0 || c.Timeout > 10*time.Minute {
		return invalid("invalid image engine configuration")
	}
	return nil
}
