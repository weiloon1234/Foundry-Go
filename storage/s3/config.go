// Package s3 implements AWS S3, an explicit Cloudflare R2 profile and a generic
// S3-compatible profile using the official AWS SDK. Buckets are pre-existing;
// this package never provisions or deletes buckets, changes ACLs, or changes
// their access policies.
package s3

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type Provider uint8

const (
	AWS Provider = iota + 1
	R2
	// Compatible is a generic S3-compatible service such as MinIO, DigitalOcean
	// Spaces or Backblaze B2. Nothing is inferred from its endpoint: optional
	// guarantees are declared explicitly in Config.Compatible.
	Compatible
)

// CompatibleCapabilities declares what a Compatible provider guarantees. Only
// declare a feature the service actually enforces; an undeclared option fails
// as Unsupported before provider I/O. Ranges and conditional reads are always
// required. SingleRequestConditions limits conditional writes to one PutObject
// (declared size within PartBytes), as for providers without conditional
// multipart completion. ListingPathEncoding selects path-style decoding of
// url-encoded listing keys instead of AWS form encoding ('+' as space).
// FormUploads declares support for browser POST-policy uploads.
type CompatibleCapabilities struct {
	ConditionalCreate, ConditionalReplace, ConditionalDelete bool
	Versions                                                 bool
	SingleRequestConditions                                  bool
	RequiresNFCKeys                                          bool
	ServerCopy                                               bool
	ListingPathEncoding                                      bool
	FormUploads                                              bool
}

const MinPartBytes int64 = 5 << 20
const MaxPartBytes int64 = 64 << 20
const MaxParts = 10000

// Config describes one bucket namespace. AWS uses its standard credential chain
// unless Credentials or Profile is supplied. R2 requires explicit Credentials
// and an HTTPS account endpoint; it never falls back to AWS machine credentials.
// Compatible requires an explicit Endpoint and explicit Credentials (never a
// Profile or the AWS default chain, which would disclose the host's AWS machine
// credentials to another provider); AllowHTTP opts it into a plain-HTTP
// endpoint for local development only (never use it across a network).
// HTTPClient, when supplied, is borrowed and must honor request cancellation.
// Only general-purpose AWS buckets are supported; directory buckets and access
// point ARNs have different protocol semantics and are rejected.
//
// Each active upload retains one PartBytes buffer when PartConcurrency is 1
// and up to PartConcurrency+1 (parts in flight plus the one filling) above
// that, or a single smaller buffer for a declared Size below PartBytes;
// MaxUploads bounds active uploads across all
// disks sharing this backend and queues briefly before failing as overloaded.
// VerifyPublication pins a HEAD to the acknowledged ETag/version to read the
// provider's Last-Modified; when disabled or refused (write-only credentials),
// the acknowledged publication is returned with the response Date as Modified.
type Config struct {
	// PublicBase points to the bucket root; Namespace is appended automatically.
	PublicBase     storage.PublicBase
	Provider       Provider
	Bucket         string
	Region         string
	Endpoint       string
	Namespace      storage.Prefix
	Profile        string
	Credentials    aws.CredentialsProvider
	HTTPClient     aws.HTTPClient
	PathStyle      bool
	PartBytes      int64
	MaxUploads     int
	MaxObjectBytes int64
	ReadAttempts   int
	PartAttempts   int
	AbortTimeout   time.Duration
	// PartConcurrency bounds concurrent multipart part uploads per object.
	PartConcurrency   int
	VerifyPublication bool
	AllowHTTP         bool
	Compatible        CompatibleCapabilities
}

func DefaultConfig(bucket, region string) Config {
	return Config{Provider: AWS, Bucket: bucket, Region: region, PartBytes: 8 << 20, MaxUploads: 4, MaxObjectBytes: 1 << 30, ReadAttempts: 3, PartAttempts: 3, AbortTimeout: 15 * time.Second, PartConcurrency: 1, VerifyPublication: true}
}

// CompatibleConfig selects a generic S3-compatible endpoint with explicitly
// declared capabilities. Path-style addressing is used by default.
func CompatibleConfig(bucket, region, endpoint string, capabilities CompatibleCapabilities) Config {
	config := DefaultConfig(bucket, region)
	config.Provider = Compatible
	config.Endpoint = endpoint
	config.PathStyle = true
	config.Compatible = capabilities
	return config
}
func R2Config(bucket, endpoint string, credentials aws.CredentialsProvider) Config {
	config := DefaultConfig(bucket, "auto")
	config.Provider = R2
	config.Endpoint = endpoint
	config.Credentials = credentials
	config.PathStyle = true
	return config
}
func (c Config) Validate() error {
	invalid := func() error {
		return storage.Failure(storage.Invalid, storage.OpenOperation, storage.NotApplicable, nil)
	}
	if c.Provider != AWS && c.Provider != R2 && c.Provider != Compatible || len(c.Bucket) < 3 || len(c.Bucket) > 63 || c.Region == "" || len(c.Region) > 128 || c.PartBytes < MinPartBytes || c.PartBytes > MaxPartBytes || c.MaxUploads < 1 || c.MaxUploads > 64 || c.MaxObjectBytes < 1 || c.MaxObjectBytes > c.PartBytes*MaxParts || c.ReadAttempts < 1 || c.ReadAttempts > 5 || c.PartAttempts < 1 || c.PartAttempts > 5 || c.AbortTimeout <= 0 || c.AbortTimeout > time.Minute || c.PartConcurrency < 1 || c.PartConcurrency > 16 {
		return invalid()
	}
	if c.Provider != Compatible && (c.AllowHTTP || c.Compatible != (CompatibleCapabilities{})) || c.Provider == Compatible && c.Endpoint == "" {
		return invalid()
	}
	if strings.HasSuffix(c.Bucket, "--x-s3") || strings.HasSuffix(c.Bucket, "-s3alias") || strings.HasSuffix(c.Bucket, "--ol-s3") || strings.HasSuffix(c.Bucket, "--table-s3") || strings.Contains(c.Bucket, "..") || net.ParseIP(c.Bucket) != nil {
		return invalid()
	}
	for i, b := range []byte(c.Bucket) {
		if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || i > 0 && i < len(c.Bucket)-1 && (b == '-' || b == '.')) {
			return invalid()
		}
	}
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if err := validateKeyText(c.requiresNFC(), c.Namespace.String()); err != nil {
		return err
	}
	if c.Namespace.String() != "" && !strings.HasSuffix(c.Namespace.String(), "/") {
		return invalid()
	}
	for _, text := range []string{c.Region, c.Profile} {
		for _, ch := range text {
			if ch < 32 || ch == 127 {
				return invalid()
			}
		}
	}
	if c.Provider == R2 && (c.Endpoint == "" || c.Region != "auto" || c.Credentials == nil || c.Profile != "") {
		return invalid()
	}
	// Like R2, a compatible service must never receive the host's AWS
	// machine credentials (environment, IMDS, ECS or SSO) from the SDK chain.
	if c.Provider == Compatible && (c.Credentials == nil || c.Profile != "") {
		return invalid()
	}
	if c.Endpoint != "" {
		parsed, err := url.Parse(c.Endpoint)
		if err != nil || parsed.Scheme != "https" && !(c.AllowHTTP && parsed.Scheme == "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
			return invalid()
		}
		if c.Provider == R2 && (!strings.HasSuffix(parsed.Hostname(), ".r2.cloudflarestorage.com") || parsed.Port() != "") {
			return invalid()
		}
	}
	return nil
}
