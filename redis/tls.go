package redis

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// TLS can replace delegate errors with strings. Retain the first failure on its
// connection, and expose only opaque errors until go-redis returns to Foundry.
// The same owner covers session tickets processed by Read after the handshake.
type tlsBoundary struct {
	mu      sync.Mutex
	failure error
}

func (b *tlsBoundary) protect(fn func() error) error {
	err := callback.Isolated("Redis TLS delegate", fn)
	if err != nil {
		b.mu.Lock()
		if b.failure == nil {
			b.failure = err
		}
		b.mu.Unlock()
	}
	return safeerror.Seal("Redis TLS delegate failed", err)
}

// Delegates without error returns must abort TLS instead of publishing a bogus
// clock, public key or cache result. run contains this framework-only panic.
func (b *tlsBoundary) require(fn func() error) {
	if b.protect(fn) != nil {
		panic("Redis TLS delegate failed")
	}
}

func (b *tlsBoundary) hasFailure() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failure != nil
}

func (b *tlsBoundary) finish(native error, panicked bool) error {
	b.mu.Lock()
	failure := b.failure
	b.mu.Unlock()
	if failure == nil && !panicked {
		return native
	}
	var contained error
	if panicked {
		contained = safeerror.Panic("Redis TLS operation panicked", nil)
	}
	return safeerror.Seal("Redis TLS operation failed", safeerror.Join("Redis TLS operation failed", failure, contained, native))
}

func (b *tlsBoundary) run(fn func() error) error {
	var native error
	contained := callback.Isolated("Redis TLS operation", func() error { native = fn(); return nil })
	b.mu.Lock()
	failure := b.failure
	b.mu.Unlock()
	if failure == nil && contained == nil {
		return native
	}
	return safeerror.Seal("Redis TLS operation failed", safeerror.Join("Redis TLS operation failed", failure, contained, native))
}

func tlsDialer(config *tls.Config, timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		raw, err := (&net.Dialer{Timeout: timeout, KeepAlive: 5 * time.Minute}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		boundary := &tlsBoundary{}
		conn := &containedTLSConn{Conn: tls.Client(raw, boundary.config(config)), raw: raw, boundary: boundary}
		if err := boundary.run(func() error { return conn.HandshakeContext(ctx) }); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	}
}

type containedTLSConn struct {
	*tls.Conn
	raw      net.Conn
	boundary *tlsBoundary
}

func (c *containedTLSConn) Read(data []byte) (n int, err error) {
	var panicked bool
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
		if panicked || c.boundary.hasFailure() {
			_ = c.raw.Close()
			err = c.boundary.finish(err, panicked)
		}
	}()
	n, err = c.Conn.Read(data)
	return n, err
}

func (c *containedTLSConn) Write(data []byte) (n int, err error) {
	var panicked bool
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
		if panicked || c.boundary.hasFailure() {
			_ = c.raw.Close()
			err = c.boundary.finish(err, panicked)
		}
	}()
	n, err = c.Conn.Write(data)
	return n, err
}

func (c *containedTLSConn) Close() error {
	defer c.raw.Close()
	return c.boundary.run(c.Conn.Close)
}

func (b *tlsBoundary) config(config *tls.Config) *tls.Config {
	config = config.Clone()
	if fn := config.VerifyPeerCertificate; fn != nil {
		config.VerifyPeerCertificate = func(raw [][]byte, chains [][]*x509.Certificate) error {
			return b.protect(func() error { return fn(raw, chains) })
		}
	}
	if fn := config.VerifyConnection; fn != nil {
		config.VerifyConnection = func(state tls.ConnectionState) error {
			return b.protect(func() error { return fn(state) })
		}
	}
	if fn := config.EncryptedClientHelloRejectionVerify; fn != nil {
		config.EncryptedClientHelloRejectionVerify = func(state tls.ConnectionState) error {
			return b.protect(func() error { return fn(state) })
		}
	}
	if fn := config.GetClientCertificate; fn != nil {
		config.GetClientCertificate = func(info *tls.CertificateRequestInfo) (certificate *tls.Certificate, err error) {
			err = b.protect(func() error { certificate, err = fn(info); return err })
			if err == nil && certificate != nil {
				certificate = b.certificate(certificate)
			}
			return certificate, err
		}
	}
	config.Certificates = append([]tls.Certificate(nil), config.Certificates...)
	for i := range config.Certificates {
		config.Certificates[i] = *b.certificate(&config.Certificates[i])
	}
	if config.Rand != nil {
		config.Rand = tlsReader{b, config.Rand}
	}
	if config.KeyLogWriter != nil {
		config.KeyLogWriter = tlsWriter{b, config.KeyLogWriter}
	}
	if fn := config.Time; fn != nil {
		config.Time = func() (value time.Time) { b.require(func() error { value = fn(); return nil }); return }
	}
	if config.ClientSessionCache != nil {
		config.ClientSessionCache = tlsSessionCache{b, config.ClientSessionCache}
	}
	return config
}

func (b *tlsBoundary) certificate(input *tls.Certificate) *tls.Certificate {
	copy := *input
	// crypto.Decrypter-only keys stay unwrapped because a client key is only used for signing.
	if signer, ok := copy.PrivateKey.(crypto.Signer); ok {
		copy.PrivateKey = tlsSigner{b, signer}
	}
	return &copy
}

type tlsReader struct {
	boundary *tlsBoundary
	reader   io.Reader
}

func (r tlsReader) Read(p []byte) (n int, err error) {
	err = r.boundary.protect(func() error { n, err = r.reader.Read(p); return err })
	return
}

type tlsWriter struct {
	boundary *tlsBoundary
	writer   io.Writer
}

func (w tlsWriter) Write(p []byte) (n int, err error) {
	err = w.boundary.protect(func() error { n, err = w.writer.Write(p); return err })
	return
}

type tlsSigner struct {
	boundary *tlsBoundary
	signer   crypto.Signer
}

func (s tlsSigner) Public() (key crypto.PublicKey) {
	s.boundary.require(func() error { key = s.signer.Public(); return nil })
	return
}
func (s tlsSigner) Sign(random io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	err = s.boundary.protect(func() error { signature, err = s.signer.Sign(random, digest, opts); return err })
	return
}
func (s tlsSigner) SignMessage(random io.Reader, message []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	err = s.boundary.protect(func() error { signature, err = crypto.SignMessage(s.signer, random, message, opts); return err })
	return
}

type tlsSessionCache struct {
	boundary *tlsBoundary
	cache    tls.ClientSessionCache
}

func (c tlsSessionCache) Get(key string) (state *tls.ClientSessionState, ok bool) {
	c.boundary.require(func() error { state, ok = c.cache.Get(key); return nil })
	return
}
func (c tlsSessionCache) Put(key string, state *tls.ClientSessionState) {
	c.boundary.require(func() error { c.cache.Put(key, state); return nil })
}
