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
	observed atomic.Bool
}

// requestDeadlineConn retains timeout evidence before HTTP framing can replace
// the native read error. Each installed upload deadline gets its own record.
type requestDeadlineConn struct {
	net.Conn
	mu     sync.Mutex
	upload *connectionReadTimeout
}

func (c *requestDeadlineConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if native, ok := err.(*net.OpError); ok && native.Op == "read" && native.Err == os.ErrDeadlineExceeded {
		c.mu.Lock()
		if c.upload != nil && !time.Now().Before(c.upload.deadline) {
			c.upload.observed.Store(true)
		}
		c.mu.Unlock()
	}
	return n, err
}

func (c *requestDeadlineConn) uploadReadDeadline(deadline time.Time) (*connectionReadTimeout, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Conn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	c.upload = &connectionReadTimeout{deadline: deadline}
	return c.upload, nil
}

// Native header, keep-alive, body and hijack deadline changes invalidate the
// upload association. They must not attribute a later timeout to an old request.
func (c *requestDeadlineConn) SetReadDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	c.upload = nil
	return nil
}

func (c *requestDeadlineConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Conn.SetDeadline(deadline); err != nil {
		return err
	}
	c.upload = nil
	return nil
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
