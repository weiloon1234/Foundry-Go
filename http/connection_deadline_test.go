package http

import (
	"net"
	"os"
	"testing"
	"testing/iotest"
	"time"
)

func TestInactiveConnectionDeadlineResetsDoNotLock(t *testing.T) {
	for _, previousUpload := range []bool{false, true} {
		conn := &requestDeadlineConn{Conn: deadlineErrorConn{}}
		if previousUpload {
			if _, err := conn.uploadReadDeadline(time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				t.Fatal(err)
			}
		}
		conn.mu.Lock()
		done := make(chan error, 1)
		go func() {
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				done <- err
				return
			}
			done <- conn.SetDeadline(time.Time{})
		}()
		select {
		case err := <-done:
			conn.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			conn.mu.Unlock()
			<-done
			t.Fatal("ordinary deadline reset acquired the upload mutex")
		}
	}
}

type blockedDeadlineConn struct {
	deadlineErrorConn
	entered chan struct{}
	release chan struct{}
}

func (c blockedDeadlineConn) SetReadDeadline(time.Time) error {
	close(c.entered)
	<-c.release
	return nil
}

func TestConnectionResetInvalidatesOverlappingUploadInstallation(t *testing.T) {
	underlying := blockedDeadlineConn{
		deadlineErrorConn: deadlineErrorConn{reader: iotest.ErrReader(&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded})},
		entered:           make(chan struct{}), release: make(chan struct{}),
	}
	conn := &requestDeadlineConn{Conn: underlying}
	done := make(chan *connectionReadTimeout, 1)
	go func() { record, _ := conn.uploadReadDeadline(time.Now().Add(-time.Second)); done <- record }()
	<-underlying.entered
	// SetDeadline forwards independently while installation is still in flight.
	reset := make(chan error, 1)
	go func() { reset <- conn.SetDeadline(time.Time{}) }()
	var err error
	select {
	case err = <-reset:
	case <-time.After(time.Second):
		close(underlying.release)
		<-done
		<-reset
		t.Fatal("reset blocked behind the upload installation")
	}
	close(underlying.release)
	record := <-done
	if err != nil || record == nil {
		t.Fatal("deadline setup failed", err)
	}
	_, _ = conn.Read(make([]byte, 1))
	if record.observed.Load() || conn.upload.Load() != nil {
		t.Fatal("reset revived an overlapping upload deadline")
	}
}
