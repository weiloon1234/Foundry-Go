package s3_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	cloudcredentials "github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func TestSameBucketCopyUsesPinnedServerSideCopy(t *testing.T) {
	digest := sha256.Sum256([]byte("abc"))
	var heads, copies, others atomic.Int32
	disk, backend := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "HEAD":
			heads.Add(1)
			wireHeaders(w, 3, `"one"`)
			w.Header().Set("X-Amz-Meta-Foundry-Sha256", hex.EncodeToString(digest[:]))
		case r.Method == "PUT" && r.Header.Get("X-Amz-Copy-Source") != "":
			copies.Add(1)
			if r.Header.Get("X-Amz-Copy-Source") != "fixture-bucket/isolated/from/a%2Bb.txt" || r.Header.Get("X-Amz-Copy-Source-If-Match") != `"one"` || r.URL.Path != "/fixture-bucket/isolated/to/copy.txt" {
				t.Error("server copy lost its source pin or identity", r.Header.Get("X-Amz-Copy-Source"), r.URL.Path)
			}
			// Like a streamed copy: the content type, proven checksum and
			// requested metadata only; source headers are never inherited.
			if r.Header.Get("X-Amz-Metadata-Directive") != "REPLACE" || r.Header.Get("Content-Type") != "text/plain" || r.Header.Get("X-Amz-Meta-Foundry-Sha256") != hex.EncodeToString(digest[:]) || r.Header.Get("Cache-Control") != "" {
				t.Error("server copy metadata differs from a streamed copy", r.Header)
			}
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<CopyObjectResult><ETag>"copied"</ETag><LastModified>2023-11-14T22:13:20Z</LastModified></CopyObjectResult>`)
		default:
			others.Add(1)
			wireError(w, 400, "UnexpectedFixtureOperation")
		}
	}), func(c *s3.Config) { c.Namespace, _ = storage.ParsePrefix("isolated/") })
	other, err := storage.NewDisk("other", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close(context.Background()) })
	source, _ := storage.ParseKey("from/a+b.txt")
	target, _ := storage.ParseKey("to/copy.txt")
	copied, err := disk.CopyTo(t.Context(), source, other, target, storage.CopyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checksum, ok := copied.Object.Checksum.Get()
	if copied.Disk != "other" || copied.Object.Key != target || copied.Object.Size != 3 || copied.Object.ETag != `"copied"` || !ok || checksum != storage.SHA256(digest) {
		t.Fatal("server copy misreported the destination", copied)
	}
	if heads.Load() != 1 || copies.Load() != 1 || others.Load() != 0 {
		t.Fatal("same-bucket copy streamed through the process", heads.Load(), copies.Load(), others.Load())
	}
	if disk.Stats().Active != 0 || other.Stats().Active != 0 {
		t.Fatal("server copy retained capacity")
	}
}

func TestCompatibleProfileDeclaresCapabilitiesAndOptInPlainHTTP(t *testing.T) {
	declared := s3.CompatibleCapabilities{ConditionalCreate: true, Versions: true}
	config := s3.CompatibleConfig("fixture-bucket", "us-east-1", "http://127.0.0.1:9000", declared)
	config.AllowHTTP = true
	// Without explicit credentials the SDK chain would send the host's AWS
	// machine credentials (environment, IMDS, ECS, SSO) to this provider.
	if err := config.Validate(); err == nil {
		t.Fatal("compatible profile accepted the AWS default credential chain")
	}
	config = config.WithCredentials(cloudcredentials.ProviderFunc(func(context.Context) (cloudcredentials.Value, error) {
		return cloudcredentials.Value{AccessKey: secret.New("fixture-key"), SecretKey: secret.New("fixture-secret")}, nil
	}))
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	profiled := config
	profiled.Profile = "default"
	if err := profiled.Validate(); err == nil {
		t.Fatal("compatible profile accepted a shared AWS profile")
	}
	config.AllowHTTP = false
	if err := config.Validate(); err == nil {
		t.Fatal("plain-HTTP endpoint accepted without explicit opt-in")
	}
	aws := s3.DefaultConfig("fixture-bucket", "us-east-1")
	aws.AllowHTTP = true
	if err := aws.Validate(); err == nil {
		t.Fatal("plain HTTP accepted outside the compatible profile")
	}
	aws.AllowHTTP, aws.Compatible = false, declared
	if err := aws.Validate(); err == nil {
		t.Fatal("compatible declarations accepted for AWS")
	}
	var calls atomic.Int32
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		wireHeaders(w, 1, `"one"`)
	}), func(c *s3.Config) {
		c.Provider, c.Compatible = s3.Compatible, declared
	})
	capabilities := disk.Capabilities()
	if !capabilities.ConditionalCreate || capabilities.ConditionalReplace || capabilities.ConditionalDelete || !capabilities.Versions || capabilities.ConditionalVersionDelete {
		t.Fatal("compatible capabilities were inferred instead of declared", capabilities)
	}
	if err := disk.Delete(t.Context(), objectKey(t), storage.DeleteOptions{IfMatch: `"one"`}); !errors.Is(err, storage.Unsupported) || calls.Load() != 0 {
		t.Fatal("undeclared conditional delete reached the provider", err)
	}
	if _, err := disk.Stat(t.Context(), objectKey(t), storage.ReadOptions{}); err != nil || calls.Load() != 1 {
		t.Fatal("compatible endpoint request failed", err)
	}
}

func TestServerCopyEnforcesDiskObjectLimitsBeforePublishing(t *testing.T) {
	var copies atomic.Int32
	disk, backend := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "HEAD":
			wireHeaders(w, 64, `"one"`)
		case r.Header.Get("X-Amz-Copy-Source") != "":
			copies.Add(1)
			wireError(w, 400, "UnexpectedFixtureOperation")
		default:
			wireError(w, 400, "UnexpectedFixtureOperation")
		}
	}), nil)
	config := storage.DefaultConfig()
	config.MaxObjectBytes = 32
	small, err := storage.NewDisk("small", backend, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = small.Close(context.Background()) })
	source, _ := storage.ParseKey("from/large.bin")
	target, _ := storage.ParseKey("to/large.bin")
	for _, pair := range [][2]*storage.Disk{{disk, small}, {small, disk}} {
		_, err := pair[0].CopyTo(t.Context(), source, pair[1], target, storage.CopyOptions{})
		var failure *storage.Error
		if !errors.As(err, &failure) || failure.Code() != storage.LimitExceeded || failure.Outcome() != storage.Unchanged {
			t.Fatal("oversized server copy was not rejected before publication", err)
		}
	}
	result, err := disk.MoveTo(t.Context(), source, small, target, storage.CopyOptions{})
	if err == nil || result.Destination.IsSet() || result.SourceOutcome != storage.Unchanged {
		t.Fatal("oversized move published or deleted anything", err)
	}
	if copies.Load() != 0 {
		t.Fatal("provider copy ran for an oversized source")
	}
}
