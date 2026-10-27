package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func tlsEchoServer(b testing.TB) (string, *x509.CertPool) {
	b.Helper()
	testServer := httptest.NewUnstartedServer(nil)
	testServer.TLS = &tls.Config{}
	testServer.StartTLS()
	roots := x509.NewCertPool()
	roots.AddCert(testServer.Certificate())
	serverCert := testServer.TLS.Certificates[0]
	testServer.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}
	tlsListener := tls.NewListener(listener, tlsConfig)
	go func() {
		for {
			conn, err := tlsListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					if _, err := c.Write(buf[:n]); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	b.Cleanup(func() {
		_ = tlsListener.Close()
	})
	return listener.Addr().String(), roots
}

func BenchmarkTLSReadWrite(b *testing.B) {
	addr, roots := tlsEchoServer(b)
	dialer := tlsDialer(&tls.Config{
		RootCAs:            roots,
		InsecureSkipVerify: true,
	}, 5*time.Second)

	conn, err := dialer(context.Background(), "tcp", addr)
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	payload := []byte("PING\r\n")
	buf := make([]byte, 64)

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()

	for b.Loop() {
		if _, err := conn.Write(payload); err != nil {
			b.Fatal(err)
		}
		n, err := conn.Read(buf)
		if err != nil {
			b.Fatal(err)
		}
		if string(buf[:n]) != string(payload) {
			b.Fatalf("unexpected read: %q", string(buf[:n]))
		}
	}
}
