package s3_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	cloudcredentials "github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
	"github.com/weiloon1234/Foundry-Go/value"
)

type noNetwork struct{ calls int }

func (c *noNetwork) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, errors.New("unexpected network request")
}
func TestR2ProfilePresigningAndPublicAccessIntent(t *testing.T) {
	transport := &noNetwork{}
	config := s3.R2WithCredentials("fixture-bucket", "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com", cloudcredentials.ProviderFunc(func(context.Context) (cloudcredentials.Value, error) {
		return cloudcredentials.Value{AccessKey: secret.New("fixture-key"), SecretKey: secret.New("fixture-secret")}, nil
	}))
	config.HTTPClient = transport
	config.Namespace, _ = storage.ParsePrefix("isolated/")
	config.PublicBase, _ = storage.ParsePublicBase("https://files.example/")
	backend, err := s3.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	private, err := storage.NewDisk("private", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer private.Close(context.Background())
	key, err := storage.ParseKey("path/a +%2f?#.txt")
	if err != nil {
		t.Fatal(err)
	}
	link, err := private.TemporaryURL(t.Context(), key, storage.LinkOptions{ExpiresIn: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link.URL())
	if err != nil || parsed.Path != "/fixture-bucket/isolated/"+key.String() {
		t.Fatal("presigned URL changed key", err)
	}
	if !strings.Contains(parsed.Query().Get("X-Amz-Credential"), "/auto/s3/") || parsed.Query().Get("X-Amz-Expires") != "300" {
		t.Fatal("R2 signing region or expiry changed")
	}
	if _, err := private.PublicURL(t.Context(), key); !errors.Is(err, storage.Forbidden) {
		t.Fatal("private disk exposed stable URL", err)
	}
	cfg := storage.DefaultConfig()
	cfg.Visibility = storage.Public
	public, err := storage.NewDisk("public", backend, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer public.Close(context.Background())
	address, err := public.PublicURL(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = url.Parse(address)
	if err != nil || parsed.Host != "files.example" || parsed.Path != "/isolated/"+key.String() {
		t.Fatal("CDN URL changed key", err)
	}
	if _, err := private.Put(t.Context(), key, strings.NewReader("value"), storage.PutOptions{Condition: storage.IfAbsent()}); !errors.Is(err, storage.Unsupported) {
		t.Fatal("conditional unknown-length stream accepted", err)
	}
	if transport.calls != 0 {
		t.Fatal("signing/capability checks touched provider")
	}
}
func TestSigningExpiryNeverExceedsTemporaryCredentials(t *testing.T) {
	config := s3.DefaultConfig("fixture-bucket", "us-east-1")
	config.HTTPClient = &noNetwork{}
	config.Credentials = aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "fixture-key", SecretAccessKey: "fixture-secret", SessionToken: "fixture-session", CanExpire: true, Expires: time.Now().Add(30 * time.Second)}, nil
	})
	backend, err := s3.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	link, err := backend.TemporaryURL(t.Context(), objectKey(t), storage.LinkOptions{ExpiresIn: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link.URL())
	if err != nil {
		t.Fatal(err)
	}
	seconds, err := strconv.Atoi(parsed.Query().Get("X-Amz-Expires"))
	if err != nil || seconds < 1 || seconds > 30 {
		t.Fatal("credential expiry ignored")
	}
}

func TestTypedEndpointIgnoresSDKEnvironmentOverride(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL", "https://unexpected.example")
	t.Setenv("AWS_ENDPOINT_URL_S3", "https://unexpected-s3.example")
	config := s3.DefaultConfig("fixture-bucket", "us-east-1")
	config.Credentials = credentials.NewStaticCredentialsProvider("fixture-key", "fixture-secret", "")
	config.HTTPClient = &noNetwork{}
	backend, err := s3.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	link, err := backend.TemporaryURL(t.Context(), objectKey(t), storage.LinkOptions{ExpiresIn: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link.URL())
	if err != nil || !strings.HasSuffix(parsed.Hostname(), ".amazonaws.com") {
		t.Fatal("environment redirected the typed storage endpoint")
	}
}
func TestS3MoveRejectsNamespaceAliasesBeforeIO(t *testing.T) {
	transport := &noNetwork{}
	config := s3.DefaultConfig("fixture-bucket", "us-east-1")
	config.Credentials = credentials.NewStaticCredentialsProvider("fixture-key", "fixture-secret", "")
	config.HTTPClient = transport
	sourceConfig := config
	sourceConfig.Namespace, _ = storage.ParsePrefix("folder/")
	sourceBackend, err := s3.Open(t.Context(), sourceConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceBackend.Close()
	targetBackend, err := s3.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer targetBackend.Close()
	source, err := storage.NewDisk("source", sourceBackend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close(context.Background())
	target, err := storage.NewDisk("target", targetBackend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(context.Background())
	from, _ := storage.ParseKey("object")
	to, _ := storage.ParseKey("folder/object")
	if _, err := source.MoveTo(t.Context(), from, target, to, storage.CopyOptions{}); !errors.Is(err, storage.Invalid) || transport.calls != 0 {
		t.Fatal("aliased move reached provider", err)
	}
}

func presignDisk(t *testing.T, configure func(*s3.Config)) *storage.Disk {
	t.Helper()
	config := s3.DefaultConfig("fixture-bucket", "us-east-1")
	config.HTTPClient = &noNetwork{}
	config = config.WithCredentials(cloudcredentials.ProviderFunc(func(context.Context) (cloudcredentials.Value, error) {
		return cloudcredentials.Value{AccessKey: secret.New("fixture-key"), SecretKey: secret.New("fixture-secret")}, nil
	}))
	if configure != nil {
		configure(&config)
	}
	backend, err := s3.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := storage.NewDisk("uploads", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = disk.Close(context.Background())
		_ = backend.Close()
	})
	return disk
}

func TestPresignedUploadSignsExactSizeTypeChecksumAndCondition(t *testing.T) {
	disk := presignDisk(t, nil)
	digest := storage.SHA256(sha256.Sum256([]byte("fixture")))
	link, err := disk.TemporaryUploadURL(t.Context(), objectKey(t), storage.UploadLinkOptions{ExpiresIn: 10 * time.Minute, ContentType: "image/png", Size: 42, Checksum: value.Set(digest), Condition: storage.IfAbsent()})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link.URL())
	if err != nil {
		t.Fatal(err)
	}
	signed := strings.Split(parsed.Query().Get("X-Amz-SignedHeaders"), ";")
	for _, name := range []string{"content-length", "content-type", "if-none-match", "x-amz-meta-foundry-sha256"} {
		if !slices.Contains(signed, name) {
			t.Fatal("upload constraint is not signed", name, signed)
		}
	}
	headers := link.Headers()
	if link.Method() != "PUT" || parsed.Query().Get("X-Amz-Expires") != "600" || headers.Get("Content-Type") != "image/png" || headers.Get("If-None-Match") != "*" || headers.Get("X-Amz-Meta-Foundry-Sha256") != digest.String() || headers.Get("Host") != "" || headers.Get("Content-Length") != "" {
		t.Fatal("presigned upload request changed", link.Method(), headers)
	}
	if link.ExpiresAt().Before(time.Now().Add(9*time.Minute)) || strings.Contains(fmt.Sprint(link), "X-Amz-Signature") {
		t.Fatal("upload link lifetime or redaction changed")
	}
	replace, err := storage.IfMatch(`"one"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range []storage.UploadLinkOptions{
		{ExpiresIn: time.Minute, ContentType: "image/png", Size: s3.MaxSingleUploadBytes + 1},
		{ExpiresIn: time.Minute, Size: 1},
		{ExpiresIn: storage.MaxLinkLifetime + time.Second, ContentType: "image/png", Size: 1},
		{ExpiresIn: time.Minute, ContentType: "image/png", Size: 1, Condition: replace},
		// A zero Content-Length is not signed, so the link would accept any size.
		{ExpiresIn: time.Minute, ContentType: "image/png", Size: 0},
	} {
		if _, err := disk.TemporaryUploadURL(t.Context(), objectKey(t), options); err == nil {
			t.Fatal("unbounded or unsupported upload link accepted", options.Size)
		}
	}
}

func TestPresignedReadSignsResponseOverridesAndKeepsLifetimeForRotatingCredentials(t *testing.T) {
	disk := presignDisk(t, nil)
	link, err := disk.TemporaryURL(t.Context(), objectKey(t), storage.LinkOptions{ExpiresIn: time.Hour, ResponseContentType: "application/pdf", ResponseContentDisposition: `attachment; filename="report.pdf"`})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link.URL())
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	if q.Get("response-content-type") != "application/pdf" || q.Get("response-content-disposition") != `attachment; filename="report.pdf"` {
		t.Fatal("response overrides were not signed into the link", q)
	}
	// Credentials without a known expiry get a synthetic cache refresh deadline;
	// it must not shorten a requested link lifetime.
	if q.Get("X-Amz-Expires") != "3600" {
		t.Fatal("refresh interval was treated as credential expiry", q.Get("X-Amz-Expires"))
	}
}
