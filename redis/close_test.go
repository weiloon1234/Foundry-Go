package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientCloseAfterTLSConnectionReset(t *testing.T) {
	certificate := httptest.NewTLSServer(nil)
	serverCert := certificate.TLS.Certificates[0]
	certificate.Close()
	connections := make(chan net.Conn, 1)
	host, port, roots, _ := tlsTransportServer(t, serverCert, func(conn net.Conn, args []string, _ <-chan struct{}) {
		if strings.EqualFold(args[0], "ping") {
			_, _ = io.WriteString(conn, "+PONG\r\n")
			connections <- conn
		} else {
			_, _ = io.WriteString(conn, "+OK\r\n")
		}
	})
	config := explicitConfig()
	config.Host, config.Port = host, port
	config.TLS = VerifyTLS
	config.TLSConfig = &tls.Config{RootCAs: roots}
	config.MaxConnections = 1
	client, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	if client.Stats().Idle != 1 {
		t.Fatal("client has no idle TLS connection")
	}
	select {
	case conn := <-connections:
		raw := conn.(*tls.Conn).NetConn().(*net.TCPConn)
		if err := raw.SetLinger(0); err != nil {
			t.Fatal(err)
		}
		if err := raw.Close(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive startup ping")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for range 2 {
		if err := client.Close(ctx); err != nil {
			t.Fatalf("closing server-reset idle TLS connection: %v", err)
		}
	}
}

func TestTLSClosePreservesFailures(t *testing.T) {
	for _, recorded := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "delegate"}[recorded], func(t *testing.T) {
			local, peer := net.Pipe()
			defer peer.Close()
			cause := errors.New("test transport close failure")
			var raw net.Conn = &failingCloseConn{Conn: local, cause: errors.Join(io.EOF, cause)}
			boundary := &tlsBoundary{}
			if recorded {
				raw = local
				cause = io.EOF
				_ = boundary.protect(func() error { return cause })
			}
			conn := &containedTLSConn{Conn: tls.Client(raw, &tls.Config{}), raw: raw, boundary: boundary}
			err := classify(context.Background(), conn.Close())
			if err == nil || !errors.Is(err, cause) {
				t.Fatal("connection close lost genuine transport or recorded delegate failure")
			}
		})
	}
}

type failingCloseConn struct {
	net.Conn
	cause error
}

func (c *failingCloseConn) Close() error { return errors.Join(c.Conn.Close(), c.cause) }
