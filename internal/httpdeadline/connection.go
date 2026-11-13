package httpdeadline

import (
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type ReadTimeout struct {
	deadline   time.Time
	generation uint64
	armed      atomic.Bool
	observed   atomic.Bool
}

func (r *ReadTimeout) Observe(err error) {
	if native, ok := err.(*net.OpError); ok && native.Op == "read" && native.Err == os.ErrDeadlineExceeded && !time.Now().Before(r.deadline) {
		r.observed.Store(true)
	}
}

// Conn retains timeout evidence before HTTP framing can replace
// the native read error. Each installed upload deadline gets its own record.
type Conn struct {
	net.Conn
	mu         sync.Mutex
	upload     atomic.Pointer[ReadTimeout]
	generation atomic.Uint64
	resets     atomic.Int64
}

func (c *Conn) stableGeneration() (uint64, bool) {
	generation := c.generation.Load()
	return generation, c.resets.Load() == 0 && c.generation.Load() == generation
}

func (c *Conn) currentUpload(record *ReadTimeout) bool {
	if record == nil || !record.armed.Load() || c.upload.Load() != record {
		return false
	}
	generation, stable := c.stableGeneration()
	return stable && generation == record.generation
}

func (c *Conn) Read(p []byte) (int, error) {
	record := c.upload.Load()
	if !c.currentUpload(record) {
		record = nil
	}
	n, err := c.Conn.Read(p)
	if c.currentUpload(record) {
		record.Observe(err)
	}
	return n, err
}

func (c *Conn) UploadReadDeadline(deadline time.Time) (*ReadTimeout, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	generation, stable := c.stableGeneration()
	record := &ReadTimeout{deadline: deadline, generation: generation}
	// Publish before the syscall so a concurrent reset can invalidate this
	// installation. Never restore a record that a reset has withdrawn.
	c.upload.Store(record)
	if err := c.Conn.SetReadDeadline(deadline); err != nil {
		c.upload.CompareAndSwap(record, nil)
		return nil, err
	}
	current, unchanged := c.stableGeneration()
	if stable && unchanged && generation == current {
		record.armed.Store(true)
	} else {
		c.upload.CompareAndSwap(record, nil)
	}
	return record, nil
}

// Native header, keep-alive, body and hijack deadline changes invalidate the
// upload association. They must not attribute a later timeout to an old request.
// Invalidating on both sides also retires overlapping installations without
// taking the upload mutex on ordinary connections.
func (c *Conn) beginReset() {
	// A count, rather than odd/even generation parity, keeps overlapping
	// resets unstable until every underlying deadline change has returned.
	c.resets.Add(1)
	c.generation.Add(1)
	c.upload.Store(nil)
}

func (c *Conn) endReset() {
	c.upload.Store(nil)
	c.generation.Add(1)
	c.resets.Add(-1)
}

func (c *Conn) SetReadDeadline(deadline time.Time) error {
	c.beginReset()
	err := c.Conn.SetReadDeadline(deadline)
	c.endReset()
	return err
}

func (c *Conn) SetDeadline(deadline time.Time) error {
	c.beginReset()
	err := c.Conn.SetDeadline(deadline)
	c.endReset()
	return err
}

type Listener struct{ net.Listener }

func (l Listener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &Conn{Conn: conn}, nil
}

func NewReadTimeout(deadline time.Time) *ReadTimeout {
	return &ReadTimeout{deadline: deadline}
}

func (r *ReadTimeout) Observed() bool { return r.observed.Load() }
