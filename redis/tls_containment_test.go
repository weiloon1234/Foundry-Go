package redis

import (
	"bufio"
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
)

// Exercise the actual TLS dial owned by Start, including go-redis's native
// logging and retry inspection before Foundry receives the error.
func TestStartContainsTLSCallbacksBeforeNativeRedis(t *testing.T) {
	for _, hook := range []string{"peer", "connection", "certificate"} {
		for _, mode := range []string{"cycle", "panic", "goexit", "callback-panic", "callback-goexit", "fault", "cancellation"} {
			t.Run(hook+"/"+mode, func(t *testing.T) {
				server := httptest.NewUnstartedServer(nil)
				server.Config.ErrorLog = log.New(io.Discard, "", 0)
				server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
				server.StartTLS()
				defer server.Close()
				host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
				number, _ := strconv.Atoi(port)
				roots := x509.NewCertPool()
				roots.AddCert(server.Certificate())
				config := DefaultConfig()
				config.Host, config.Port = host, uint16(number)
				config.ConnectTimeout = 2 * time.Second
				config.TLSConfig = &tls.Config{RootCAs: roots}
				cause := &errorgraphtest.Error{Mode: mode}
				var calls atomic.Int32
				var input error = cause
				switch mode {
				case "fault":
					input = fault.Wrap(fault.Conflict, "private TLS fault", cause)
				case "cancellation":
					input = &privateTLSCancellation{cause: cause}
				}
				fail := func() error {
					calls.Add(1)
					if mode == "callback-panic" {
						panic("private callback")
					}
					if mode == "callback-goexit" {
						runtime.Goexit()
					}
					return input
				}
				switch hook {
				case "peer":
					config.TLSConfig.VerifyPeerCertificate = func([][]byte, [][]*x509.Certificate) error { return fail() }
				case "connection":
					config.TLSConfig.VerifyConnection = func(tls.ConnectionState) error { return fail() }
				case "certificate":
					config.TLSConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return nil, fail() }
				}
				client, err := Prepare(config)
				if err != nil {
					t.Fatal(err)
				}
				err = client.Start(t.Context())
				if err == nil || calls.Load() == 0 {
					t.Fatal("TLS callback not exercised")
				}
				if strings.Contains(err.Error(), "private") || cause.Formatted.Load() != 0 {
					t.Fatal("native TLS/Redis exposed callback text")
				}
				if strings.HasPrefix(mode, "callback-") {
					if !errors.Is(err, fault.Panicked) {
						t.Fatal("callback failure lost panic identity")
					}
				} else if !errors.Is(err, input) {
					t.Fatal("TLS failure lost identity")
				}
				if mode == "fault" && !errors.Is(err, fault.Conflict) {
					t.Fatal("fault classification lost")
				}
				if mode == "cancellation" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation identity lost")
				}
				closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := client.Close(closeCtx); err != nil {
					t.Fatal("TLS failure retained client ownership")
				}
				if client.Stats().Operations != 0 || cause.Visits.Load() > 2048 {
					t.Fatal("unbounded native error inspection")
				}
			})
		}
	}
}

type privateTLSCancellation struct{ cause *errorgraphtest.Error }

func (e *privateTLSCancellation) Error() string {
	e.cause.Formatted.Add(1)
	return "private cancellation"
}
func (*privateTLSCancellation) Is(target error) bool { return target == context.Canceled }

// These failures cross native paths which stringify (and sometimes replace)
// errors. Time/cache/Public have no error return and must abort the operation.
func TestStartContainsTLSDelegates(t *testing.T) {
	for _, hook := range []string{"rand", "key-log", "sign", "selected-sign", "message-sign", "public", "selected-public", "time", "cache-get", "cache-put", "cache-put-tls13"} {
		modes := []string{"returned", "panic", "goexit"}
		if hook == "time" || hook == "public" || hook == "selected-public" || strings.HasPrefix(hook, "cache-") {
			modes = modes[1:]
		}
		for _, mode := range modes {
			t.Run(hook+"/"+mode, func(t *testing.T) {
				server := httptest.NewUnstartedServer(nil)
				server.Config.ErrorLog = log.New(io.Discard, "", 0)
				server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert, MaxVersion: tls.VersionTLS12}
				if hook == "cache-put-tls13" {
					server.TLS.MaxVersion = tls.VersionTLS13
				}
				server.StartTLS()
				defer server.Close()
				host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
				number, _ := strconv.Atoi(port)
				roots := x509.NewCertPool()
				roots.AddCert(server.Certificate())
				config := DefaultConfig()
				config.Host, config.Port, config.ConnectTimeout = host, uint16(number), time.Second
				config.TLSConfig = &tls.Config{RootCAs: roots, MaxVersion: server.TLS.MaxVersion}
				cause := &errorgraphtest.Error{Mode: "cycle"}
				var calls atomic.Int32
				fail := func() error {
					calls.Add(1)
					if mode == "panic" {
						panic("private TLS delegate")
					}
					if mode == "goexit" {
						runtime.Goexit()
					}
					return cause
				}
				switch hook {
				case "rand":
					config.TLSConfig.Rand = failingTLSReader{fail}
				case "key-log":
					config.TLSConfig.KeyLogWriter = failingTLSWriter{fail}
				case "time":
					config.TLSConfig.Time = func() time.Time { _ = fail(); return time.Now() }
				case "cache-get", "cache-put", "cache-put-tls13":
					config.TLSConfig.ClientSessionCache = failingTLSCache{fail: fail, get: hook == "cache-get"}
				default:
					certificate := server.TLS.Certificates[0]
					signer := failingTLSSigner{signer: certificate.PrivateKey.(crypto.Signer), fail: fail, public: strings.Contains(hook, "public")}
					certificate.PrivateKey = signer
					if hook == "message-sign" {
						certificate.PrivateKey = failingTLSMessageSigner{signer}
					}
					if strings.HasPrefix(hook, "selected-") {
						config.TLSConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &certificate, nil }
					} else {
						config.TLSConfig.Certificates = []tls.Certificate{certificate}
					}
				}
				client, err := Prepare(config)
				if err != nil {
					t.Fatal(err)
				}
				err = client.Start(t.Context())
				if err == nil || calls.Load() == 0 {
					t.Fatal("delegate was not exercised")
				}
				if mode == "returned" {
					var retained *errorgraphtest.Error
					if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
						t.Fatal("native TLS replacement lost delegate identity")
					}
				} else if !errors.Is(err, fault.Panicked) {
					t.Fatal("delegate lost panic classification")
				}
				_ = fmt.Sprintf("%v %+v %#v", err, err, err)
				if strings.Contains(err.Error(), "private") || cause.Formatted.Load() != 0 {
					t.Fatal("delegate text formatted or disclosed")
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := client.Close(ctx); err != nil || client.Stats().Operations != 0 {
					t.Fatal("failed TLS retained ownership")
				}
			})
		}
	}
}

type failingTLSReader struct{ fail func() error }

func (r failingTLSReader) Read([]byte) (int, error) { return 0, r.fail() }

type failingTLSWriter struct{ fail func() error }

func (w failingTLSWriter) Write([]byte) (int, error) { return 0, w.fail() }

type failingTLSSigner struct {
	signer crypto.Signer
	fail   func() error
	public bool
}

func (s failingTLSSigner) Public() crypto.PublicKey {
	if s.public {
		_ = s.fail()
	}
	return s.signer.Public()
}
func (s failingTLSSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, s.fail()
}

type reconnectSigner struct {
	signer     crypto.Signer
	failActive *atomic.Bool
	fail       func() error
}

func (s reconnectSigner) Public() crypto.PublicKey {
	return s.signer.Public()
}

func (s reconnectSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if s.failActive.Load() {
		return nil, s.fail()
	}
	return s.signer.Sign(rand, digest, opts)
}

type failingTLSMessageSigner struct{ failingTLSSigner }

func (s failingTLSMessageSigner) SignMessage(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, s.fail()
}

type failingTLSCache struct {
	fail func() error
	get  bool
}

func (c failingTLSCache) Get(string) (*tls.ClientSessionState, bool) {
	if c.get {
		_ = c.fail()
	}
	return nil, false
}
func (c failingTLSCache) Put(string, *tls.ClientSessionState) {
	if !c.get {
		_ = c.fail()
	}
}

func TestReadTimeoutLeavesConnectionUsable(t *testing.T) {
	server := httptest.NewUnstartedServer(nil)
	server.TLS = &tls.Config{}
	server.StartTLS()
	defer server.Close()

	serverCert := server.TLS.Certificates[0]
	roots := x509.NewCertPool()
	cert, err := x509.ParseCertificate(serverCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(cert)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}
	tlsListener := tls.NewListener(listener, tlsConfig)
	defer tlsListener.Close()

	go func() {
		conn, err := tlsListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	dialer := tlsDialer(&tls.Config{
		RootCAs:            roots,
		InsecureSkipVerify: true,
	}, 2*time.Second)

	conn, err := dialer(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 1. A read timeout must NOT close the raw connection.
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if n != 0 || err == nil {
		t.Fatalf("expected timeout error, got n=%d err=%v", n, err)
	}
	netErr, ok := err.(net.Error)
	if !ok || !netErr.Timeout() {
		t.Fatalf("expected timeout net.Error, got %v", err)
	}

	// 2. Proving the connection is still usable: extend deadline, write and read.
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("PING\r\n")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write after read timeout failed: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read after read timeout failed: %v", err)
	}
	if string(buf[:n]) != "PING\r\n" {
		t.Fatalf("unexpected echo data: %q", string(buf[:n]))
	}
}

func TestClientReadTimeoutSurfacesTimeout(t *testing.T) {
	server := httptest.NewUnstartedServer(nil)
	server.TLS = &tls.Config{}
	server.StartTLS()
	defer server.Close()

	var started atomic.Bool
	host, port, roots, _ := tlsTransportServer(t, server.TLS.Certificates[0], func(conn net.Conn, args []string, done <-chan struct{}) {
		if strings.EqualFold(args[0], "ping") {
			if started.CompareAndSwap(false, true) {
				io.WriteString(conn, "+PONG\r\n")
				return
			}
			<-done
		}
	})

	config := DefaultConfig()
	config.Host, config.Port = host, port
	config.ConnectTimeout = 2 * time.Second
	config.TLSConfig = &tls.Config{RootCAs: roots}

	client, err := Prepare(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err = client.Ping(ctx)
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, fault.Timeout)) {
		t.Fatalf("expected deadline/timeout error, got %v", err)
	}

	ctx2, cancel2 := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel2()
	if err := client.Close(ctx2); err != nil {
		t.Fatal("client close failed after timeout", err)
	}
}

func TestDelegateFailsAfterStartupOnReconnect(t *testing.T) {
	for _, hook := range []string{"signer", "session-cache", "clock"} {
		for _, mode := range []string{"returned", "panic", "goexit"} {
			if hook != "signer" && mode == "returned" {
				continue
			}
			t.Run(hook+"/"+mode, func(t *testing.T) {
				server := httptest.NewUnstartedServer(nil)
				server.TLS = &tls.Config{}
				server.StartTLS()
				defer server.Close()

				serverCert := server.TLS.Certificates[0]
				var failActive atomic.Bool
				cause := &errorgraphtest.Error{Mode: "cycle"}

				fail := func() error {
					if mode == "panic" {
						panic("private reconnect panic")
					}
					if mode == "goexit" {
						runtime.Goexit()
					}
					return cause
				}

				host, port, roots, closeConns := tlsTransportServer(t, serverCert, func(conn net.Conn, args []string, done <-chan struct{}) {
					if strings.EqualFold(args[0], "ping") {
						io.WriteString(conn, "+PONG\r\n")
					}
				})

				config := DefaultConfig()
				config.Host, config.Port = host, port
				config.ConnectTimeout = 2 * time.Second
				config.TLSConfig = &tls.Config{RootCAs: roots}

				switch hook {
				case "signer":
					clientCert := serverCert
					clientCert.PrivateKey = reconnectSigner{
						signer:     clientCert.PrivateKey.(crypto.Signer),
						failActive: &failActive,
						fail:       fail,
					}
					config.TLSConfig.Certificates = []tls.Certificate{clientCert}
				case "session-cache":
					config.TLSConfig.ClientSessionCache = failingTLSCache{
						get: true,
						fail: func() error {
							if failActive.Load() {
								return fail()
							}
							return nil
						},
					}
				case "clock":
					config.TLSConfig.Time = func() time.Time {
						if failActive.Load() {
							_ = fail()
						}
						return time.Now()
					}
				}

				client, err := Prepare(config)
				if err != nil {
					t.Fatal(err)
				}
				if err := client.Start(t.Context()); err != nil {
					t.Fatalf("client.Start failed: %v", err)
				}

				failActive.Store(true)
				closeConns()

				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				// First ping discovers the closed connection and evicts it from the pool.
				_ = client.Ping(ctx)

				// Subsequent ping must dial a new connection, triggering the delegate failure.
				err = client.Ping(ctx)
				if err == nil {
					t.Fatal("expected delegate error on reconnect")
				}

				if mode == "returned" {
					var retained *errorgraphtest.Error
					if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
						t.Fatalf("lost delegate identity: %v", err)
					}
				} else if !errors.Is(err, fault.Panicked) {
					t.Fatalf("lost panic classification: %v", err)
				}

				_ = fmt.Sprintf("%v %+v %#v", err, err, err)
				if strings.Contains(err.Error(), "private") || cause.Formatted.Load() != 0 {
					t.Fatal("delegate text formatted or disclosed")
				}

				closeCtx, cancelClose := context.WithTimeout(context.Background(), time.Second)
				defer cancelClose()
				if err := client.Close(closeCtx); err != nil || client.Stats().Operations != 0 {
					t.Fatal("failed TLS retained ownership")
				}
				if cause.Visits.Load() > 2048 {
					t.Fatal("unbounded native error inspection")
				}
			})
		}
	}
}

func TestRecordedDelegateFailureSurfacesOnClose(t *testing.T) {
	server := httptest.NewUnstartedServer(nil)
	server.TLS = &tls.Config{}
	server.StartTLS()
	defer server.Close()

	serverCert := server.TLS.Certificates[0]
	roots := x509.NewCertPool()
	cert, err := x509.ParseCertificate(serverCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(cert)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}
	tlsListener := tls.NewListener(listener, tlsConfig)
	defer tlsListener.Close()

	go func() {
		conn, err := tlsListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	cause := &errorgraphtest.Error{Mode: "cycle"}
	dialer := tlsDialer(&tls.Config{
		RootCAs:            roots,
		InsecureSkipVerify: true,
	}, 2*time.Second)

	rawConn, err := dialer(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn := rawConn.(*containedTLSConn)

	// Simulate a delegate failure recorded on the boundary
	conn.boundary.protect(func() error { return cause })

	err = classify(context.Background(), conn.Close())
	if err == nil {
		t.Fatal("expected error on close with delegate failure")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("lost error identity on close: %v", err)
	}
	_ = fmt.Sprintf("%v %+v %#v", err, err, err)
	if cause.Formatted.Load() != 0 || strings.Contains(err.Error(), "private") {
		t.Fatal("delegate text formatted or disclosed")
	}
}

func TestRecordedDelegateFailureClosesRawConnectionOnReadWrite(t *testing.T) {
	for _, op := range []string{"read", "write"} {
		t.Run(op, func(t *testing.T) {
			server := httptest.NewUnstartedServer(nil)
			server.TLS = &tls.Config{}
			server.StartTLS()
			defer server.Close()

			serverCert := server.TLS.Certificates[0]
			roots := x509.NewCertPool()
			cert, err := x509.ParseCertificate(serverCert.Certificate[0])
			if err != nil {
				t.Fatal(err)
			}
			roots.AddCert(cert)

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{serverCert},
			}
			tlsListener := tls.NewListener(listener, tlsConfig)
			defer tlsListener.Close()

			go func() {
				conn, err := tlsListener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				buf := make([]byte, 1024)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					if _, err := conn.Write(buf[:n]); err != nil {
						return
					}
				}
			}()

			cause := &errorgraphtest.Error{Mode: "cycle"}
			dialer := tlsDialer(&tls.Config{
				RootCAs:            roots,
				InsecureSkipVerify: true,
			}, 2*time.Second)

			rawConn, err := dialer(t.Context(), "tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer rawConn.Close()
			conn := rawConn.(*containedTLSConn)

			var opErr error
			if op == "read" {
				if _, err := conn.Write([]byte("PING\r\n")); err != nil {
					t.Fatal(err)
				}
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				conn.boundary.protect(func() error { return cause })
				buf := make([]byte, 64)
				_, opErr = conn.Read(buf)
			} else {
				conn.boundary.protect(func() error { return cause })
				_, opErr = conn.Write([]byte("PING\r\n"))
			}

			if opErr == nil {
				t.Fatalf("expected error on %s after recorded delegate failure", op)
			}

			_ = fmt.Sprintf("%v %+v %#v", opErr, opErr, opErr)
			if strings.Contains(opErr.Error(), "private") || cause.Formatted.Load() != 0 {
				t.Fatal("delegate text formatted or disclosed")
			}

			var probeBuf [1]byte
			_, rawErr := conn.raw.Read(probeBuf[:])
			if rawErr == nil || (!errors.Is(rawErr, net.ErrClosed) && !strings.Contains(rawErr.Error(), "closed")) {
				t.Fatalf("raw connection was not closed after %s: %v", op, rawErr)
			}

			revealed := classify(context.Background(), opErr)
			var retained *errorgraphtest.Error
			if !errors.Is(revealed, cause) || !errors.As(revealed, &retained) || retained != cause {
				t.Fatalf("lost delegate error identity on %s: %v", op, revealed)
			}
		})
	}
}

func TestEncryptedClientHelloRejectionVerifyContainment(t *testing.T) {
	t.Run("returned-error", func(t *testing.T) {
		cause := &errorgraphtest.Error{Mode: "cycle"}
		boundary := &tlsBoundary{}
		cfg := &tls.Config{
			EncryptedClientHelloRejectionVerify: func(tls.ConnectionState) error {
				return cause
			},
		}
		wrapped := boundary.config(cfg)
		err := wrapped.EncryptedClientHelloRejectionVerify(tls.ConnectionState{})
		if err == nil {
			t.Fatal("expected error")
		}
		// Foreign crypto/tls sees sealed error without formatting
		_ = fmt.Sprintf("%v %+v %#v", err, err, err)
		if strings.Contains(err.Error(), "private") || cause.Formatted.Load() != 0 {
			t.Fatal("delegate text formatted or disclosed")
		}
		if !boundary.hasFailure() {
			t.Fatal("boundary did not record failure")
		}
		// Classified error reveals identity to Foundry
		revealed := classify(context.Background(), err)
		var retained *errorgraphtest.Error
		if !errors.Is(revealed, cause) || !errors.As(revealed, &retained) || retained != cause {
			t.Fatalf("lost error identity: %v", revealed)
		}
	})

	t.Run("panic", func(t *testing.T) {
		boundary := &tlsBoundary{}
		cfg := &tls.Config{
			EncryptedClientHelloRejectionVerify: func(tls.ConnectionState) error {
				panic("private ech rejection panic")
			},
		}
		wrapped := boundary.config(cfg)
		err := wrapped.EncryptedClientHelloRejectionVerify(tls.ConnectionState{})
		if err == nil {
			t.Fatal("expected error")
		}
		_ = fmt.Sprintf("%v %+v %#v", err, err, err)
		if strings.Contains(err.Error(), "private") {
			t.Fatal("disclosed private panic message")
		}
		if !boundary.hasFailure() {
			t.Fatal("boundary did not record failure")
		}
		revealed := classify(context.Background(), err)
		if !errors.Is(revealed, fault.Panicked) {
			t.Fatalf("lost panic classification: %v", revealed)
		}
	})

	t.Run("goexit", func(t *testing.T) {
		boundary := &tlsBoundary{}
		cfg := &tls.Config{
			EncryptedClientHelloRejectionVerify: func(tls.ConnectionState) error {
				runtime.Goexit()
				return nil
			},
		}
		wrapped := boundary.config(cfg)
		err := wrapped.EncryptedClientHelloRejectionVerify(tls.ConnectionState{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !boundary.hasFailure() {
			t.Fatal("boundary did not record failure")
		}
		revealed := classify(context.Background(), err)
		if !errors.Is(revealed, fault.Panicked) {
			t.Fatalf("lost goexit panic classification: %v", revealed)
		}
	})
}

func tlsTransportServer(t *testing.T, serverCert tls.Certificate, command func(net.Conn, []string, <-chan struct{})) (string, uint16, *x509.CertPool, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequestClientCert,
	}
	tlsListener := tls.NewListener(listener, tlsConfig)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var connections []net.Conn
	done := make(chan struct{})
	wg.Go(func() {
		for {
			conn, err := tlsListener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			wg.Go(func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					args, err := readCommand(reader)
					if err != nil {
						return
					}
					if strings.EqualFold(args[0], "hello") {
						io.WriteString(conn, "*0\r\n")
						continue
					}
					command(conn, args, done)
				}
			})
		}
	})
	closeConns := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			conn.Close()
		}
		connections = nil
	}
	cleanup := func() {
		close(done)
		tlsListener.Close()
		closeConns()
		wg.Wait()
	}
	t.Cleanup(cleanup)
	addr := listener.Addr().(*net.TCPAddr)
	roots := x509.NewCertPool()
	cert, err := x509.ParseCertificate(serverCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(cert)
	return addr.IP.String(), uint16(addr.Port), roots, closeConns
}
