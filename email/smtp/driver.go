// Package smtp sends one MIME message over an owned SMTP connection. TLS is
// required except for an explicit loopback-only development mode.
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
	"strconv"
	"strings"
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

type Config struct {
	Address   string
	Security  Security
	Username  string
	Password  secret.String
	TLSConfig *tls.Config
	Timeout   time.Duration
}

func (Config) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("SMTP configuration")) }

type Driver struct {
	address, host, username string
	password                secret.String
	security                Security
	tls                     *tls.Config
	timeout                 time.Duration
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
	if (config.Username == "") != config.Password.IsZero() || len(config.Username) > 512 || len(config.Password.Reveal()) > 4096 || strings.ContainsAny(config.Username+config.Password.Reveal(), "\r\n\x00") {
		return nil, email.Construction
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
	return &Driver{address: config.Address, host: host, username: config.Username, password: config.Password, security: config.Security, tls: tlsConfig, timeout: config.Timeout}, nil
}
func (Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("SMTP driver")) }
func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	if ctx.Err() != nil {
		return email.Receipt{}, email.Transient
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	connection, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, "tcp", d.address)
	if err != nil {
		return email.Receipt{}, email.Transient
	}
	defer connection.Close()
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(closed); _ = connection.Close() })
	defer func() {
		if !stop() {
			<-closed
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		if connection.SetDeadline(deadline) != nil {
			return email.Receipt{}, email.Transient
		}
	}
	var wire net.Conn = connection
	if d.security == TLS {
		encrypted := tls.Client(connection, d.tls.Clone())
		if err := encrypted.HandshakeContext(ctx); err != nil {
			return email.Receipt{}, tlsFailure(err)
		}
		wire = encrypted
	}
	client, err := stdsmtp.NewClient(wire, d.host)
	if err != nil {
		return email.Receipt{}, beforeData(err)
	}
	defer client.Close()
	if d.security == STARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return email.Receipt{}, email.Permanent
		}
		if err := client.StartTLS(d.tls.Clone()); err != nil {
			return email.Receipt{}, tlsFailure(err)
		}
	}
	if d.username != "" {
		if err := client.Auth(stdsmtp.PlainAuth("", d.username, d.password.Reveal(), d.host)); err != nil {
			return email.Receipt{}, beforeData(err)
		}
	}
	m := out.Message()
	if err := client.Mail(m.From().Mailbox()); err != nil {
		return email.Receipt{}, beforeData(err)
	}
	// All recipients must be accepted before DATA. Never report a whole-message
	// retryable failure after partial delivery to an accepted subset.
	for _, recipient := range m.EnvelopeRecipients() {
		if err := client.Rcpt(recipient.Mailbox()); err != nil {
			return email.Receipt{}, beforeData(err)
		}
	}
	data, err := client.Data()
	if err != nil {
		return email.Receipt{}, beforeData(err)
	}
	if _, err := data.Write(out.MIME()); err != nil {
		return email.Receipt{}, email.Ambiguous
	}
	if err := data.Close(); err != nil {
		var status *textproto.Error
		if errors.As(err, &status) {
			return email.Receipt{}, beforeData(err)
		}
		return email.Receipt{}, email.Ambiguous
	}
	// The DATA reply acknowledged acceptance. QUIT/connection-close failure must
	// not turn that into a retry and send the same message again.
	return email.Receipt{}, nil
}
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
