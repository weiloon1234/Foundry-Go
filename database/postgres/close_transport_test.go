package postgres_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/postgres"
)

// postgresTLSBridge supplies TLS even when the existing test PostgreSQL has it
// disabled. It forwards real protocol traffic and closes only its own sockets
// when PostgreSQL terminates a backend; it never starts another database server.
func postgresTLSBridge(t *testing.T, upstream postgres.Config) (func(*postgres.Config), <-chan struct{}) {
	t.Helper()
	certificate := httptest.NewTLSServer(nil)
	serverTLS := certificate.TLS.Clone()
	roots := x509.NewCertPool()
	roots.AddCert(certificate.Certificate())
	certificate.Close()
	native, err := postgres.TestConnectionConfig(upstream)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var sockets []net.Conn
	disconnected := make(chan struct{}, 8)
	wg.Go(func() {
		for {
			downstream, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			sockets = append(sockets, downstream)
			mu.Unlock()
			wg.Go(func() {
				defer downstream.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				_ = downstream.SetDeadline(time.Now().Add(20 * time.Second))
				var request [8]byte
				if _, err := io.ReadFull(downstream, request[:]); err != nil || request != [8]byte{0, 0, 0, 8, 4, 210, 22, 47} {
					return
				}
				dialer := net.Dialer{}
				backend, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(upstream.Host, strconv.Itoa(int(upstream.Port))))
				if err != nil {
					return
				}
				defer backend.Close()
				mu.Lock()
				sockets = append(sockets, backend)
				mu.Unlock()
				_ = backend.SetDeadline(time.Now().Add(20 * time.Second))
				if native.TLSConfig != nil {
					if _, err := backend.Write(request[:]); err != nil {
						return
					}
					var response [1]byte
					if _, err := io.ReadFull(backend, response[:]); err != nil || response[0] != 'S' {
						return
					}
					backend = tls.Client(backend, native.TLSConfig)
				}
				if _, err := downstream.Write([]byte{'S'}); err != nil {
					return
				}
				secured := tls.Server(downstream, serverTLS)
				if err := secured.HandshakeContext(ctx); err != nil {
					return
				}
				copied := make(chan struct{})
				go func() {
					defer close(copied)
					_, _ = io.Copy(backend, secured)
					_ = backend.Close()
				}()
				_, _ = io.Copy(secured, backend)
				// Reset the transport after backend exit so the client's TLS
				// close_notify deterministically encounters the dead connection.
				_ = downstream.(*net.TCPConn).SetLinger(0)
				_ = downstream.Close()
				<-copied
				disconnected <- struct{}{}
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for _, socket := range sockets {
			_ = socket.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	address := listener.Addr().(*net.TCPAddr)
	return func(config *postgres.Config) {
		config.Host, config.Port = "127.0.0.1", uint16(address.Port)
		config.TLS = postgres.VerifyFull
		config.TLSConfig = &tls.Config{RootCAs: roots}
	}, disconnected
}
