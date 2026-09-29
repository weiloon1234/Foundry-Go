// Package smtp sends MIME messages over SMTP connections owned by the driver.
// TLS is required except for an explicit loopback-only development mode.
// Optionally, a bounded pool reuses idle connections between messages.
package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	stdsmtp "net/smtp"
	"net/textproto"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Security uint8

const (
	STARTTLS Security = iota + 1
	TLS
	PlainLoopback
)

// Mechanism selects SMTP authentication. Empty selects PLAIN when a username
// is configured. XOAUTH2 sends Password as the OAuth 2.0 access token.
type Mechanism string

const (
	AuthPlain   Mechanism = "plain"
	AuthLogin   Mechanism = "login"
	AuthXOAuth2 Mechanism = "xoauth2"
)

// Config addresses one SMTP relay. LocalName is the EHLO/HELO identity; empty
// uses this machine's host name (never "localhost"), or the connection's local
// address literal when the host name is unusable. MaxIdle > 0 keeps up to that
// many authenticated connections for IdleTimeout (default 30 seconds) and
// resets them with RSET before reuse; zero opens one connection per message.
type Config struct {
	Address     string
	Security    Security
	Username    string
	Password    secret.String
	Auth        Mechanism
	LocalName   string
	TLSConfig   *tls.Config
	Timeout     time.Duration
	MaxIdle     int
	IdleTimeout time.Duration
}

func (Config) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("SMTP configuration")) }

type Driver struct {
	address, host, username, localName string
	password                           secret.String
	auth                               Mechanism
	security                           Security
	tls                                *tls.Config
	timeout, idleTimeout               time.Duration
	maxIdle                            int
	mu                                 sync.Mutex
	idle                               []*connection
	closed                             bool
}

// connection is one established, authenticated SMTP session.
type connection struct {
	raw    net.Conn
	client *stdsmtp.Client
	idle   time.Time
}

func (c *connection) close() {
	_ = c.client.Close()
	_ = c.raw.Close()
}

func New(config Config) (*Driver, error) {
	host, port, err := net.SplitHostPort(config.Address)
	number, numberErr := strconv.Atoi(port)
	if err != nil || host == "" || strings.ContainsAny(host, "\r\n\x00") || numberErr != nil || number < 1 || number > 65535 || config.Timeout <= 0 || config.Timeout > 10*time.Minute {
		return nil, email.Construction
	}
	if config.Security != STARTTLS && config.Security != TLS && config.Security != PlainLoopback {
		return nil, email.Construction
	}
	if (config.Username == "") != config.Password.IsZero() || len(config.Username) > 512 || len(config.Password.Reveal()) > 8192 || strings.ContainsAny(config.Username+config.Password.Reveal(), "\r\n\x00") {
		return nil, email.Construction
	}
	switch config.Auth {
	case "":
		if config.Username != "" {
			config.Auth = AuthPlain
		}
	case AuthPlain, AuthLogin, AuthXOAuth2:
		if config.Username == "" {
			return nil, email.Construction
		}
	default:
		return nil, email.Construction
	}
	if config.LocalName != "" && !validLocalName(config.LocalName) || config.MaxIdle < 0 || config.MaxIdle > 64 || config.IdleTimeout < 0 || config.IdleTimeout > time.Hour {
		return nil, email.Construction
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 30 * time.Second
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if config.TLSConfig != nil {
		tlsConfig = config.TLSConfig.Clone()
		if tlsConfig.InsecureSkipVerify || tlsConfig.MinVersion != 0 && tlsConfig.MinVersion < tls.VersionTLS12 {
			return nil, email.Construction
		}
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = host
		}
		if tlsConfig.MinVersion == 0 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	if config.Security == PlainLoopback {
		ip := net.ParseIP(host)
		if !(host == "localhost" || ip != nil && ip.IsLoopback()) || config.Username != "" {
			return nil, email.Construction
		}
	}
	localName := config.LocalName
	if localName == "" {
		if name, err := os.Hostname(); err == nil && validLocalName(name) && !strings.EqualFold(name, "localhost") {
			localName = name
		}
	}
	return &Driver{address: config.Address, host: host, username: config.Username, password: config.Password, auth: config.Auth, localName: localName, security: config.Security, tls: tlsConfig, timeout: config.Timeout, idleTimeout: config.IdleTimeout, maxIdle: config.MaxIdle}, nil
}

// validLocalName accepts a domain-like EHLO identity or an address literal.
func validLocalName(name string) bool {
	if len(name) == 0 || len(name) > 255 {
		return false
	}
	for _, c := range name {
		if c <= ' ' || c > '~' {
			return false
		}
	}
	return true
}
func (*Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("SMTP driver")) }

// Close releases idle pooled connections and stops pooling. Close it after the
// borrowing mailer has drained; an in-flight send still closes its connection.
func (d *Driver) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	idle := d.idle
	d.idle, d.closed = nil, true
	d.mu.Unlock()
	for _, c := range idle {
		c.close()
	}
}

func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	if ctx.Err() != nil {
		return email.Receipt{}, email.Transient
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	session, reused, err := d.acquire(ctx)
	if err != nil {
		return email.Receipt{}, err
	}
	receipt, reusable, err := d.deliver(ctx, session, out)
	if reused && errors.Is(err, errStale) {
		// The pooled session failed on its first command without a server
		// reply; nothing was submitted, so one retry on a fresh session is safe.
		session.close()
		if session, err = d.dial(ctx); err != nil {
			return email.Receipt{}, err
		}
		receipt, reusable, err = d.deliver(ctx, session, out)
	}
	if reusable {
		d.release(session)
	} else {
		session.close()
	}
	if errors.Is(err, errStale) {
		err = email.Transient
	}
	return receipt, err
}

var errStale = errors.New("pooled SMTP session is unusable")

// deliver runs one transaction. reusable reports that the session remains in
// a clean state after acceptance and can return to the pool.
func (d *Driver) deliver(ctx context.Context, session *connection, out email.Outbound) (email.Receipt, bool, error) {
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(closed); _ = session.raw.Close() })
	interrupted := func() bool {
		if stop() {
			return false
		}
		<-closed
		return true
	}
	if deadline, ok := ctx.Deadline(); ok {
		if session.raw.SetDeadline(deadline) != nil {
			interrupted()
			return email.Receipt{}, false, email.Transient
		}
	}
	client := session.client
	m := out.Message()
	if err := client.Mail(m.From().Mailbox()); err != nil {
		interrupted()
		return email.Receipt{}, false, staleOr(err)
	}
	// All recipients must be accepted before DATA. Never report a whole-message
	// retryable failure after partial delivery to an accepted subset.
	for _, recipient := range m.EnvelopeRecipients() {
		if err := client.Rcpt(recipient.Mailbox()); err != nil {
			interrupted()
			return email.Receipt{}, false, beforeData(err)
		}
	}
	data, err := client.Data()
	if err != nil {
		interrupted()
		return email.Receipt{}, false, beforeData(err)
	}
	if _, err := data.Write(out.MIME()); err != nil {
		interrupted()
		return email.Receipt{}, false, email.Ambiguous
	}
	if err := data.Close(); err != nil {
		interrupted()
		var status *textproto.Error
		if errors.As(err, &status) {
			return email.Receipt{}, false, beforeData(err)
		}
		return email.Receipt{}, false, email.Ambiguous
	}
	// The DATA reply acknowledged acceptance. A later reset/close failure must
	// not turn that into a retry and send the same message again.
	if interrupted() || session.raw.SetDeadline(time.Time{}) != nil {
		return email.Receipt{}, false, nil
	}
	return email.Receipt{}, true, nil
}

// staleOr marks a MAIL failure without a server reply as a stale session.
func staleOr(err error) error {
	var status *textproto.Error
	if errors.As(err, &status) {
		return beforeData(err)
	}
	return errStale
}

// acquire returns a live pooled session (after RSET) or dials a new one.
func (d *Driver) acquire(ctx context.Context) (*connection, bool, error) {
	for {
		d.mu.Lock()
		var session *connection
		if n := len(d.idle); n > 0 {
			session = d.idle[n-1]
			d.idle = d.idle[:n-1]
		}
		d.mu.Unlock()
		if session == nil {
			fresh, err := d.dial(ctx)
			return fresh, false, err
		}
		if time.Since(session.idle) >= d.idleTimeout {
			session.close()
			continue
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(d.timeout)
		}
		if session.raw.SetDeadline(deadline) != nil || session.client.Reset() != nil {
			session.close()
			continue
		}
		return session, true, nil
	}
}

func (d *Driver) release(session *connection) {
	session.idle = time.Now()
	d.mu.Lock()
	if !d.closed && len(d.idle) < d.maxIdle {
		d.idle = append(d.idle, session)
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	session.close()
}

// dial establishes, secures and authenticates one session.
func (d *Driver) dial(ctx context.Context) (*connection, error) {
	raw, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, "tcp", d.address)
	if err != nil {
		return nil, email.Transient
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(closed); _ = raw.Close() })
	fail := func(err error) (*connection, error) {
		if !stop() {
			<-closed
		}
		_ = raw.Close()
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if raw.SetDeadline(deadline) != nil {
			return fail(email.Transient)
		}
	}
	var wire net.Conn = raw
	if d.security == TLS {
		encrypted := tls.Client(raw, d.tls.Clone())
		if err := encrypted.HandshakeContext(ctx); err != nil {
			return fail(tlsFailure(err))
		}
		wire = encrypted
	}
	client, err := stdsmtp.NewClient(wire, d.host)
	if err != nil {
		return fail(beforeData(err))
	}
	session := &connection{raw: raw, client: client}
	failSession := func(err error) (*connection, error) {
		_ = client.Close()
		return fail(err)
	}
	if err := client.Hello(d.helloName(raw)); err != nil {
		return failSession(beforeData(err))
	}
	if d.security == STARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return failSession(email.Permanent)
		}
		if err := client.StartTLS(d.tls.Clone()); err != nil {
			return failSession(tlsFailure(err))
		}
	}
	if d.auth != "" {
		// A server that does not advertise the configured mechanism (504 or no
		// AUTH extension) cannot accept this configuration; do not retry.
		ok, mechanisms := client.Extension("AUTH")
		if !ok || !slices.ContainsFunc(strings.Fields(mechanisms), func(m string) bool { return strings.EqualFold(m, string(d.auth)) }) {
			return failSession(email.Permanent)
		}
		if err := client.Auth(d.authenticator()); err != nil {
			return failSession(authFailure(err))
		}
	}
	if !stop() {
		<-closed
		_ = client.Close()
		return nil, email.Transient
	}
	return session, nil
}

func (d *Driver) helloName(raw net.Conn) string {
	if d.localName != "" {
		return d.localName
	}
	if address, ok := raw.LocalAddr().(*net.TCPAddr); ok {
		if ip := address.IP.To4(); ip != nil {
			return "[" + ip.String() + "]"
		}
		return "[IPv6:" + address.IP.String() + "]"
	}
	return "[127.0.0.1]"
}

func (d *Driver) authenticator() stdsmtp.Auth {
	switch d.auth {
	case AuthLogin:
		return loginAuth{username: d.username, password: d.password}
	case AuthXOAuth2:
		return xoauth2{username: d.username, token: d.password}
	}
	return stdsmtp.PlainAuth("", d.username, d.password.Reveal(), d.host)
}

// errUnencrypted refuses to send credentials over an unencrypted session.
var errUnencrypted = errors.New("SMTP authentication requires TLS")

type loginAuth struct {
	username string
	password secret.String
}

func (a loginAuth) Start(server *stdsmtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errUnencrypted
	}
	return "LOGIN", nil, nil
}
func (a loginAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(challenge))) {
	case "username:", "user name", "username":
		return []byte(a.username), nil
	case "password:", "password":
		return []byte(a.password.Reveal()), nil
	}
	return nil, errors.New("unexpected SMTP LOGIN challenge")
}

type xoauth2 struct {
	username string
	token    secret.String
}

func (a xoauth2) Start(server *stdsmtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errUnencrypted
	}
	return "XOAUTH2", []byte("user=" + a.username + "\x01auth=Bearer " + a.token.Reveal() + "\x01\x01"), nil
}

// Next answers a failure challenge with an empty response so the server sends
// its final error status.
func (xoauth2) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return []byte{}, nil
	}
	return nil, nil
}

// authFailure treats credential rejection (535) and unsupported mechanisms as
// permanent configuration errors; temporary 4xx failures remain transient.
func authFailure(err error) error {
	if errors.Is(err, errUnencrypted) {
		return email.Permanent
	}
	return beforeData(err)
}

// beforeData classifies failures before the message was submitted: a 5xx reply
// (including 504 "parameter not implemented") is a definite permanent
// rejection; 4xx replies and transport failures are temporary non-acceptance.
func beforeData(err error) error {
	var status *textproto.Error
	if errors.As(err, &status) && status.Code >= 500 && status.Code < 600 {
		return email.Permanent
	}
	return email.Transient
}
func tlsFailure(err error) error {
	var certificate x509.CertificateInvalidError
	var hostname x509.HostnameError
	var authority x509.UnknownAuthorityError
	if errors.As(err, &certificate) || errors.As(err, &hostname) || errors.As(err, &authority) {
		return email.Permanent
	}
	return beforeData(err)
}
