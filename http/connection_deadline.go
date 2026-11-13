package http

import (
	"context"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type connectionReadTimeout struct {
	deadline time.Time
	armed    atomic.Bool
	observed atomic.Bool
}

func (r *connectionReadTimeout) observe(err error) {
	if native, ok := err.(*net.OpError); ok && native.Op == "read" && native.Err == os.ErrDeadlineExceeded && !time.Now().Before(r.deadline) {
		r.observed.Store(true)
	}
}

// requestDeadlineConn retains timeout evidence before HTTP framing can replace
// the native read error. Each installed upload deadline gets its own record.
type requestDeadlineConn struct {
	net.Conn
	mu     sync.Mutex
	upload atomic.Pointer[connectionReadTimeout]
}

func (c *requestDeadlineConn) Read(p []byte) (int, error) {
	record := c.upload.Load()
	if record != nil && !record.armed.Load() {
		record = nil
	}
	n, err := c.Conn.Read(p)
	if record != nil && c.upload.Load() == record {
		record.observe(err)
	}
	return n, err
}

func (c *requestDeadlineConn) uploadReadDeadline(deadline time.Time) (*connectionReadTimeout, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	record := &connectionReadTimeout{deadline: deadline}
	// Publish before the syscall so a concurrent reset can invalidate this
	// installation. Never restore a record that a reset has withdrawn.
	c.upload.Store(record)
	if err := c.Conn.SetReadDeadline(deadline); err != nil {
		c.upload.CompareAndSwap(record, nil)
		return nil, err
	}
	record.armed.Store(true)
	return record, nil
}

// Native header, keep-alive, body and hijack deadline changes invalidate the
// upload association. They must not attribute a later timeout to an old request.
// Invalidating on both sides also retires overlapping installations without
// taking the upload mutex on ordinary connections.
func (c *requestDeadlineConn) SetReadDeadline(deadline time.Time) error {
	c.upload.Store(nil)
	err := c.Conn.SetReadDeadline(deadline)
	c.upload.Store(nil)
	return err
}

func (c *requestDeadlineConn) SetDeadline(deadline time.Time) error {
	c.upload.Store(nil)
	err := c.Conn.SetDeadline(deadline)
	c.upload.Store(nil)
	return err
}

type requestDeadlineListener struct{ net.Listener }

func (l requestDeadlineListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &requestDeadlineConn{Conn: conn}, nil
}

type requestDeadlineConnKey struct{}

func requestDeadlineContext(ctx context.Context, conn net.Conn) context.Context {
	if owned, ok := conn.(*requestDeadlineConn); ok {
		return context.WithValue(ctx, requestDeadlineConnKey{}, owned)
	}
	return ctx
}
