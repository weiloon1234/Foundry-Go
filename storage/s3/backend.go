package s3

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// Backend borrows supplied credentials/transports. A Disk owns operation bounds;
// MaxUploads additionally bounds shared multipart buffers across all disks using
// this adapter. Close disks and await Done before closing this backend.
type Backend struct {
	activeMu                  sync.Mutex
	activeWrites              map[storage.ObjectKey]int
	config                    Config
	lifecycle                 sync.Mutex
	prepared, started, closed bool
	client                    *awss3.Client
	credentials               aws.CredentialsProvider
	transport                 *http.Transport
	uploads                   *admission.Semaphore
	stop                      chan struct{}
	buffers                   sync.Pool
	// Replayable reads and parts share one standard retryer each, so their
	// retry quota spans calls instead of being rebuilt per request.
	readRetry, partRetry func(*awss3.Options)
}

func Prepare(config Config) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	b := &Backend{config: config, prepared: true, activeWrites: make(map[storage.ObjectKey]int), uploads: admission.New(config.MaxUploads), stop: make(chan struct{})}
	b.buffers.New = func() any { buffer := make([]byte, 0, int(config.PartBytes)); return &buffer }
	b.readRetry = safeRetry(config.ReadAttempts)
	b.partRetry = safeRetry(config.PartAttempts)
	return b, nil
}
func Open(ctx context.Context, config Config) (*Backend, error) {
	backend, err := Prepare(config)
	if err != nil {
		return nil, err
	}
	if err = backend.Start(ctx); err != nil {
		_ = backend.Close()
		return nil, err
	}
	return backend, nil
}
func (b *Backend) Start(ctx context.Context) error {
	if b == nil || ctx == nil {
		return storage.Failure(storage.Invalid, storage.OpenOperation, storage.NotApplicable, nil)
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if !b.prepared {
		return storage.Failure(storage.Invalid, storage.OpenOperation, storage.NotApplicable, nil)
	}
	if b.closed {
		return storage.Failure(storage.Closed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	if err := ctx.Err(); err != nil {
		return failure(storage.OpenOperation, storage.NotApplicable, err)
	}
	if b.started {
		return nil
	}
	client := b.config.HTTPClient
	var owned *http.Transport
	if client == nil {
		owned = &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 100, MaxIdleConnsPerHost: b.config.MaxUploads + 2, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second}
		// Request contexts own transfer deadlines, including response-body reads.
		client = &http.Client{Transport: owned, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	// Only S3 API operations disable SDK retries (below). Credential clients
	// created by configuration loading (IMDS, STS, SSO) keep their own retries.
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(b.config.Region), awsconfig.WithHTTPClient(client)}
	if b.config.Profile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(b.config.Profile))
	}
	if b.config.Credentials != nil {
		options = append(options, awsconfig.WithCredentialsProvider(b.config.Credentials))
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		if owned != nil {
			owned.CloseIdleConnections()
		}
		return failure(storage.OpenOperation, storage.NotApplicable, err)
	}
	b.client = awss3.NewFromConfig(loaded, func(o *awss3.Options) {
		o.Retryer = aws.NopRetryer{}
		o.RetryMaxAttempts = 1
		o.UsePathStyle = b.config.PathStyle
		o.BaseEndpoint = nil // Endpoint is owned by typed Foundry config, not SDK environment overrides.
		if b.config.Endpoint != "" {
			o.BaseEndpoint = aws.String(b.config.Endpoint)
		}
		// Calculate explicit Content-MD5 for parts, and never emit an unsupported
		// implicit streaming checksum/trailer into an S3-compatible provider.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		o.Logger = logging.Nop{}
		o.ClientLogMode = 0
	})
	b.credentials = loaded.Credentials
	b.transport = owned
	b.started = true
	return nil
}
func (b *Backend) Close() error {
	if b == nil {
		return nil
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if !b.closed {
		b.closed = true
		close(b.stop)
		if b.transport != nil {
			b.transport.CloseIdleConnections()
		}
	}
	return nil
}
func (b *Backend) ready(ctx context.Context, op storage.Operation) error {
	if b == nil || ctx == nil {
		return storage.Failure(storage.Invalid, op, storage.NotApplicable, nil)
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if !b.prepared || !b.started {
		return storage.Failure(storage.Invalid, op, storage.NotApplicable, nil)
	}
	if b.closed {
		return storage.Failure(storage.Closed, op, storage.NotApplicable, nil)
	}
	return failureIf(op, storage.Unchanged, ctx.Err())
}
func (b *Backend) Capabilities() storage.Capabilities {
	capabilities := storage.Capabilities{Ranges: true, ConditionalRead: true, DelimitedList: true, ObjectMetadata: true}
	if b == nil {
		return capabilities
	}
	switch b.config.Provider {
	case AWS:
		// AWS evaluates a delete condition against the current object, so a
		// version delete never carries IfMatch (ConditionalVersionDelete false).
		capabilities.ConditionalCreate = true
		capabilities.ConditionalReplace = true
		capabilities.ConditionalDelete = true
		capabilities.Versions = true
	case R2:
		// An exact size bound keeps conditional writes on supported PutObject calls.
		capabilities.RequiresNFCKeys = true
		capabilities.ConditionalCreate, capabilities.ConditionalReplace = true, true
		capabilities.ConditionalWriteMaxBytes = b.config.PartBytes
	case Compatible:
		declared := b.config.Compatible
		capabilities.RequiresNFCKeys = declared.RequiresNFCKeys
		capabilities.ConditionalCreate, capabilities.ConditionalReplace = declared.ConditionalCreate, declared.ConditionalReplace
		capabilities.ConditionalDelete, capabilities.Versions = declared.ConditionalDelete, declared.Versions
		if declared.SingleRequestConditions {
			capabilities.ConditionalWriteMaxBytes = b.config.PartBytes
		}
	}
	return capabilities
}
func (b *Backend) object(key storage.ObjectKey) (string, error) {
	if err := key.Validate(); err != nil {
		return "", err
	}
	full := b.config.Namespace.String() + key.String()
	if err := validateKeyText(b.config.requiresNFC(), full); err != nil {
		return "", err
	}
	if _, err := storage.ParseKey(full); err != nil {
		return "", err
	}
	return full, nil
}

// versionID exposes only identifiers that can select retained object versions.
// R2 may return an upload generation in a write response, but that value is not
// a supported historical-version selector. Current-object reads use the ETag.
func (b *Backend) versionID(raw string) storage.VersionID {
	if raw == "null" || !b.Capabilities().Versions {
		return ""
	}
	return storage.VersionID(raw)
}

func (b *Backend) readOptions(options storage.ReadOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	if options.Version == "null" {
		return storage.Failure(storage.Unsupported, storage.OpenOperation, storage.NotApplicable, nil)
	}
	return b.Capabilities().ValidateRead(options)
}

// safeRetry builds one reusable retryer for replayable operations. Only the
// per-operation Retryer changes; the client's single-attempt default remains
// for every mutation that is not explicitly replayable.
func safeRetry(attempts int) func(*awss3.Options) {
	retryer := retry.NewStandard(func(r *retry.StandardOptions) { r.MaxAttempts = attempts })
	return func(o *awss3.Options) { o.Retryer = retryer }
}
func failureIf(op storage.Operation, state storage.Outcome, err error) error {
	if err == nil {
		return nil
	}
	return failure(op, state, err)
}
func joinClose(op storage.Operation, state storage.Outcome, primary, cleanup error) error {
	if primary == nil && cleanup == nil {
		return nil
	}
	return failure(op, state, errors.Join(primary, cleanup))
}

var _ storage.Backend = (*Backend)(nil)
