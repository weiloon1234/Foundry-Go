// Package credentials is the framework-owned boundary shared by S3, R2 and SES.
// Applications can use static settings, the AWS chain or a rotating Provider
// without importing an SDK. Credentials are never logged or stored in reports.
package credentials

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go/logging"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Name string

func (n Name) Validate() error { return namedservice.Validate(string(n)) }

type Mode string

const (
	Chain  Mode = "chain"
	Static Mode = "static"
)

// Value supports temporary, rotating credentials and explicit expiration.
type Value struct {
	AccessKey, SecretKey, SessionToken secret.String
	Expires                            time.Time
}

func (Value) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("cloud credentials")) }
func (v Value) Validate() error {
	if v.AccessKey.IsZero() || v.SecretKey.IsZero() {
		return fault.New(fault.Invalid, "cloud credentials are incomplete")
	}
	return nil
}

// Provider implementations own refresh and must honor cancellation. Retrieve
// returns an immutable snapshot; callers never retry ambiguous delivery.
type Provider interface {
	Retrieve(context.Context) (Value, error)
}
type ProviderFunc func(context.Context) (Value, error)

func (f ProviderFunc) Retrieve(ctx context.Context) (Value, error) {
	if f == nil || ctx == nil {
		return Value{}, fault.New(fault.Invalid, "invalid credential provider")
	}
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	return f(ctx)
}

// Settings selects a reusable source. Chain preserves standard AWS discovery;
// R2 configuration requires an explicitly selected non-chain source.
//
//foundry:config
type Settings struct {
	Mode                               Mode
	Profile                            string
	AccessKey, SecretKey, SessionToken secret.String
}

func DefaultSettings() Settings { return Settings{Mode: Chain} }
func (s Settings) Validate() error {
	switch s.Mode {
	case Chain:
		if !s.AccessKey.IsZero() || !s.SecretKey.IsZero() || !s.SessionToken.IsZero() {
			return fault.New(fault.Invalid, "chain credentials cannot contain static keys")
		}
	case Static:
		if s.Profile != "" {
			return fault.New(fault.Invalid, "static credentials cannot select a profile")
		}
		return (Value{AccessKey: s.AccessKey, SecretKey: s.SecretKey, SessionToken: s.SessionToken}).Validate()
	default:
		return fault.New(fault.Invalid, "unsupported credential source")
	}
	if len(s.Profile) > 128 {
		return fault.New(fault.Invalid, "invalid AWS profile")
	}
	for _, r := range s.Profile {
		if r < 32 || r == 127 {
			return fault.New(fault.Invalid, "invalid AWS profile")
		}
	}
	return nil
}
func (Settings) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("cloud credential settings")) }

// Source owns the chain's transport. Prepare is pure; Start loads chain settings
// at boot. Retrieval/refresh occurs on demand through the existing AWS SDK.
type Source struct{ state *sourceState }
type sourceState struct {
	mu              sync.Mutex
	settings        Settings
	provider        aws.CredentialsProvider
	transport       *http.Transport
	started, closed bool
}

func Prepare(settings Settings) (*Source, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &Source{state: &sourceState{settings: settings}}, nil
}
func (s *Source) Start(ctx context.Context) error {
	if s == nil || s.state == nil || ctx == nil {
		return fault.New(fault.Invalid, "credential source needs context")
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.closed {
		return fault.New(fault.Closed, "credential source is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.state.started {
		return nil
	}
	if err := s.state.settings.Validate(); err != nil {
		return err
	}
	if s.state.settings.Mode == Chain {
		transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 16, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second}
		options := []func(*awsconfig.LoadOptions) error{awsconfig.WithHTTPClient(&http.Client{Transport: transport, Timeout: 30 * time.Second}), awsconfig.WithLogger(logging.Nop{})}
		if s.state.settings.Profile != "" {
			options = append(options, awsconfig.WithSharedConfigProfile(s.state.settings.Profile))
		}
		config, err := awsconfig.LoadDefaultConfig(ctx, options...)
		if err != nil {
			transport.CloseIdleConnections()
			return fault.Wrap(fault.Invalid, "cannot initialize cloud credential chain", err)
		}
		s.state.provider = config.Credentials
		s.state.transport = transport
	}
	s.state.started = true
	return nil
}
func (s *Source) Retrieve(ctx context.Context) (Value, error) {
	if s == nil || s.state == nil || ctx == nil {
		return Value{}, fault.New(fault.Invalid, "credential source needs context")
	}
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	s.state.mu.Lock()
	if s.state.closed || !s.state.started {
		s.state.mu.Unlock()
		return Value{}, fault.New(fault.Closed, "credential source is not active")
	}
	settings, provider := s.state.settings, s.state.provider
	s.state.mu.Unlock()
	if settings.Mode == Static {
		return Value{AccessKey: settings.AccessKey, SecretKey: settings.SecretKey, SessionToken: settings.SessionToken}, nil
	}
	result, err := provider.Retrieve(ctx)
	if err != nil {
		return Value{}, fault.Wrap(fault.Invalid, "cannot retrieve cloud credentials", err)
	}
	value := Value{AccessKey: secret.New(result.AccessKeyID), SecretKey: secret.New(result.SecretAccessKey), SessionToken: secret.New(result.SessionToken)}
	if result.CanExpire {
		value.Expires = result.Expires
	}
	if err = value.Validate(); err != nil {
		return Value{}, err
	}
	return value, nil
}
func (s *Source) Close() error {
	if s == nil || s.state == nil {
		return nil
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.closed = true
	if s.state.transport != nil {
		s.state.transport.CloseIdleConnections()
	}
	return nil
}

// Format and LogValue keep private chain/static state out of diagnostics.
func (Source) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("cloud credential source")) }
func (Source) LogValue() slog.Value       { return slog.StringValue("cloud credential source") }
