package s3

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagetest "github.com/weiloon1234/Foundry-Go/testkit/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// These opt-in tests use real providers, not the SDK wire peers. Each factory
// creates a random namespace under an explicitly selected test-only prefix.
// Cleanup checks every full key, and never provisions/resets/deletes a bucket.
func TestRealAWSStorageContract(t *testing.T) { runRealProvider(t, AWS) }
func TestRealR2StorageContract(t *testing.T)  { runRealProvider(t, R2) }
func realConfig(t *testing.T, provider Provider) Config {
	t.Helper()
	name := "S3"
	if provider == R2 {
		name = "R2"
	}
	env := "FOUNDRY_TEST_" + name + "_"
	bucket := os.Getenv(env + "BUCKET")
	if bucket == "" {
		if os.Getenv(env+"REQUIRED") == "1" {
			t.Fatal("required real provider has no configured test bucket")
		}
		t.Skip("real provider certification is not configured")
	}
	base := os.Getenv(env + "PREFIX")
	if base == "" || !strings.HasSuffix(base, "/") {
		t.Fatal("real certification requires an explicit test-only PREFIX ending in slash")
	}
	prefix, err := storage.ParsePrefix(base)
	if err != nil {
		t.Fatal("invalid real-provider test prefix")
	}
	cfg := DefaultConfig(bucket, os.Getenv(env+"REGION"))
	cfg.Profile = os.Getenv(env + "PROFILE")
	if provider == R2 {
		access, secret := os.Getenv(env+"ACCESS_KEY_ID"), os.Getenv(env+"SECRET_ACCESS_KEY")
		if access == "" || secret == "" {
			t.Fatal("real R2 certification requires explicit test credentials")
		}
		cfg = R2Config(bucket, os.Getenv(env+"ENDPOINT"), credentials.NewStaticCredentialsProvider(access, secret, os.Getenv(env+"SESSION_TOKEN")))
	}
	if address := os.Getenv(env + "PUBLIC_BASE"); address != "" {
		public, err := storage.ParsePublicBase(address)
		if err != nil {
			t.Fatal("invalid provider public base configuration")
		}
		cfg.PublicBase = public
	}
	cfg.Namespace = prefix
	cfg.PartBytes = MinPartBytes
	if err := cfg.Validate(); err != nil {
		t.Fatal("invalid real-provider configuration", err)
	}
	return cfg
}
func providerFactory(t *testing.T, cfg Config) (*storage.Disk, *Backend) {
	t.Helper()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	prefix, err := storage.ParsePrefix(cfg.Namespace.String() + "m11-" + hex.EncodeToString(random[:]) + "/")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Namespace = prefix
	backend, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var disk *storage.Disk
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if disk != nil {
			if err := disk.Close(ctx); err != nil {
				t.Error(err)
			}
		}
		if err := cleanupProvider(ctx, backend); err != nil {
			t.Error("test-owned provider cleanup failed", failure(storage.DeleteOperation, storage.Unknown, err))
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	// Establish cleanup permissions before writing any fixture objects.
	if _, err := backend.ListUploads(t.Context(), storage.ListOptions{Limit: 1}); err != nil {
		t.Fatal("certification needs multipart inspection permissions", err)
	}
	if cfg.Provider == AWS {
		if _, err := backend.client.ListObjectVersions(t.Context(), &awss3.ListObjectVersionsInput{Bucket: aws.String(cfg.Bucket), Prefix: aws.String(prefix.String()), MaxKeys: aws.Int32(1), EncodingType: types.EncodingTypeUrl}); err != nil {
			t.Fatal("certification needs version-list permissions", failure(storage.ListOperation, storage.NotApplicable, err))
		}
	}
	disk, err = storage.NewDisk("provider-test", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return disk, backend
}
func runRealProvider(t *testing.T, provider Provider) {
	cfg := realConfig(t, provider)
	storagetest.Run(t, func(t *testing.T) *storage.Disk { disk, _ := providerFactory(t, cfg); return disk })
	t.Run("multipart-and-presigned-read", func(t *testing.T) {
		disk, _ := providerFactory(t, cfg)
		key, _ := storage.ParseKey("large/space +%2f?#.bin")
		size := cfg.PartBytes + 123
		hash := sha256.New()
		if _, err := io.Copy(hash, io.LimitReader(providerZeros{}, size)); err != nil {
			t.Fatal(err)
		}
		var digest storage.SHA256
		copy(digest[:], hash.Sum(nil))
		stored, err := disk.Put(t.Context(), key, io.LimitReader(providerZeros{}, size), storage.PutOptions{Checksum: value.Set(digest)})
		if err != nil || stored.Object.Size != size {
			t.Fatal("multipart publication", err)
		}
		body, info, err := disk.Open(t.Context(), key, storage.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		count, readErr := io.Copy(io.Discard, body)
		closeErr := body.Close()
		if readErr != nil || closeErr != nil || count != size || info.Object.ETag != stored.Object.ETag {
			t.Fatal("multipart readback", readErr, closeErr)
		}
		link, err := disk.TemporaryURL(t.Context(), key, storage.LinkOptions{ExpiresIn: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		verifyProviderURL(t, link.URL(), size, digest)
	})
	t.Run("multipart-source-failure-cleans-staging", func(t *testing.T) {
		disk, backend := providerFactory(t, cfg)
		key, _ := storage.ParseKey("interrupted/stream.bin")
		sentinel := errors.New("test source interrupted")
		source := io.MultiReader(io.LimitReader(providerZeros{}, cfg.PartBytes+123), providerReadFailure{sentinel})
		_, err := disk.Put(t.Context(), key, source, storage.PutOptions{})
		var failure *storage.Error
		if !errors.Is(err, sentinel) || !errors.As(err, &failure) || failure.Outcome() != storage.Unchanged || failure.Cleanup().IsSet() {
			t.Fatal("interrupted multipart cleanup did not complete", err)
		}
		pending, err := backend.ListUploads(t.Context(), storage.ListOptions{Limit: 1})
		if err != nil || len(pending.Uploads) != 0 || !pending.Next.IsZero() {
			t.Fatal("interrupted multipart left staging", err)
		}
		if exists, err := disk.Exists(t.Context(), key); err != nil || exists {
			t.Fatal("interrupted source published an object", err)
		}
	})
	t.Run("configured-public-read", func(t *testing.T) {
		if cfg.PublicBase.IsZero() {
			t.Skip("public bucket/CDN is not configured")
		}
		disk, backend := providerFactory(t, cfg)
		key, _ := storage.ParseKey("public/space +%2f?#é.txt")
		data := []byte("Foundry-Go public URL identity fixture")
		stored, err := disk.PutBytes(t.Context(), key, data, storage.PutOptions{ContentType: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := disk.PublicURL(t.Context(), key); !errors.Is(err, storage.Forbidden) {
			t.Fatal("private intent exposed public URL", err)
		}
		options := storage.DefaultConfig()
		options.Visibility = storage.Public
		public, err := storage.NewDisk("provider-public-test", backend, options)
		if err != nil {
			t.Fatal(err)
		}
		defer public.Close(context.Background())
		address, err := public.PublicURL(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		verifyProviderURL(t, address, stored.Object.Size, storage.SHA256(sha256.Sum256(data)))
	})

	if provider == AWS {
		t.Run("encoded-listing-and-multipart-inspection", func(t *testing.T) {
			disk, backend := providerFactory(t, cfg)
			names := []string{"listing/a +%2f?#é.txt", "listing/b +%20.txt"}
			for _, name := range names {
				key, _ := storage.ParseKey(name)
				if _, err := disk.PutBytes(t.Context(), key, []byte("identity"), storage.PutOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			prefix, _ := storage.ParsePrefix("listing/")
			options := storage.ListOptions{Prefix: prefix, Limit: 1}
			for i, name := range names {
				page, err := disk.List(t.Context(), options)
				if err != nil || len(page.Objects) != 1 || page.Objects[0].Key.String() != name || page.Next.IsZero() != (i == len(names)-1) {
					t.Fatal("AWS listing changed encoded key or pagination", err)
				}
				options.Cursor = page.Next
			}
			staged := []string{"staging/a +%2f?#é.txt", "staging/b +%20.txt"}
			for _, name := range staged {
				_, err := backend.client.CreateMultipartUpload(t.Context(), &awss3.CreateMultipartUploadInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(backend.config.Namespace.String() + name)})
				if err != nil {
					t.Fatal("cannot create owned multipart fixture", failure(storage.PutOperation, storage.Unknown, err))
				}
			}
			prefix, _ = storage.ParsePrefix("staging/")
			options = storage.ListOptions{Prefix: prefix, Limit: 1}
			var references []UploadReference
			for i, name := range staged {
				page, err := backend.ListUploads(t.Context(), options)
				if err != nil || len(page.Uploads) != 1 || page.Uploads[0].Reference.Key().String() != name || page.Next.IsZero() != (i == len(staged)-1) {
					t.Fatal("AWS multipart listing changed encoded key or pagination", err)
				}
				references = append(references, page.Uploads[0].Reference)
				options.Cursor = page.Next
			}
			for _, reference := range references {
				if err := backend.AbortUpload(t.Context(), reference); err != nil {
					t.Fatal("cannot abort owned listed upload", err)
				}
			}
		})
		t.Run("immutable-version-read", func(t *testing.T) {
			disk, _ := providerFactory(t, cfg)
			key, _ := storage.ParseKey("versions/value")
			first, err := disk.PutBytes(t.Context(), key, []byte("first"), storage.PutOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if first.Object.Version == "" {
				if os.Getenv("FOUNDRY_TEST_S3_VERSIONED_REQUIRED") == "1" {
					t.Fatal("version certification requires an already-versioned test bucket")
				}
				t.Skip("bucket is unversioned; immutable-version certification pending")
			}
			if _, err := disk.PutBytes(t.Context(), key, []byte("replacement"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			old, _, err := disk.ReadBytes(t.Context(), key, 64, storage.ReadOptions{Version: first.Object.Version})
			if err != nil || string(old) != "first" {
				t.Fatal("version selection changed", err)
			}
		})
	}
}

// Never log the URL: signed links are bearer credentials.
func verifyProviderURL(t *testing.T, address string, size int64, digest storage.SHA256) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), "GET", address, nil)
	if err != nil {
		t.Fatal("cannot build provider URL request")
	}
	response, err := (&http.Client{Timeout: time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		t.Fatal("provider URL request failed")
	}
	hash := sha256.New()
	count, readErr := io.Copy(hash, io.LimitReader(response.Body, size+1))
	closeErr := response.Body.Close()
	var actual storage.SHA256
	copy(actual[:], hash.Sum(nil))
	if response.StatusCode != 200 || readErr != nil || closeErr != nil || count != size || actual != digest {
		t.Fatal("provider URL did not preserve the complete object", response.StatusCode)
	}
}

type providerReadFailure struct{ err error }

func (r providerReadFailure) Read([]byte) (int, error) { return 0, r.err }

type providerZeros struct{}

func (providerZeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func cleanupProvider(ctx context.Context, b *Backend) error {
	// Only unfinished staging in this factory's random namespace is considered.
	for round := 0; round < 8; round++ {
		page, err := b.ListUploads(ctx, storage.ListOptions{Limit: 64})
		if err != nil {
			return err
		}
		if len(page.Uploads) == 0 && page.Next.IsZero() {
			break
		}
		if round == 7 {
			return storage.LimitExceeded
		}
		for _, upload := range page.Uploads {
			if err := b.AbortUpload(ctx, upload.Reference); err != nil {
				return err
			}
		}
	}
	if b.config.Provider == AWS {
		return cleanupAWSVersions(ctx, b)
	}
	// R2 has no retained versions. Re-read the first bounded page after each batch.
	for round := 0; round < 8; round++ {
		page, err := b.List(ctx, storage.ListOptions{Limit: 64})
		if err != nil {
			return err
		}
		if len(page.Objects) == 0 && page.Next.IsZero() {
			return nil
		}
		for _, object := range page.Objects {
			if err := b.Delete(ctx, object.Key, storage.DeleteOptions{}); err != nil {
				return err
			}
		}
	}
	return storage.LimitExceeded
}
func cleanupAWSVersions(ctx context.Context, b *Backend) error {
	type target struct{ key, version string }
	var targets []target
	input := &awss3.ListObjectVersionsInput{Bucket: aws.String(b.config.Bucket), Prefix: aws.String(b.config.Namespace.String()), MaxKeys: aws.Int32(64), EncodingType: types.EncodingTypeUrl}
	add := func(encoded, version string) error {
		key, err := b.decodeListedKey(encoded)
		if err != nil || !strings.HasPrefix(key, b.config.Namespace.String()) || version == "" {
			return storage.IntegrityFailed
		}
		if _, err := storage.ParseKey(key); err != nil {
			return err
		}
		targets = append(targets, target{key, version})
		if len(targets) > 512 {
			return storage.LimitExceeded
		}
		return nil
	}
	for round := 0; round < 9; round++ {
		page, err := b.client.ListObjectVersions(ctx, input)
		if err != nil {
			return err
		}
		if page == nil || page.EncodingType != types.EncodingTypeUrl {
			return storage.IntegrityFailed
		}
		for _, version := range page.Versions {
			if err := add(aws.ToString(version.Key), aws.ToString(version.VersionId)); err != nil {
				return err
			}
		}
		for _, marker := range page.DeleteMarkers {
			if err := add(aws.ToString(marker.Key), aws.ToString(marker.VersionId)); err != nil {
				return err
			}
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		next, err := b.decodeListedKey(aws.ToString(page.NextKeyMarker))
		version := aws.ToString(page.NextVersionIdMarker)
		if err != nil || round == 8 || !strings.HasPrefix(next, b.config.Namespace.String()) || version == "" || next == aws.ToString(input.KeyMarker) && version == aws.ToString(input.VersionIdMarker) {
			return storage.IntegrityFailed
		}
		input.KeyMarker, input.VersionIdMarker = aws.String(next), aws.String(version)
	}
	// All key scopes are checked before any version/marker deletion starts.
	for _, target := range targets {
		if _, err := b.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(b.config.Bucket), Key: aws.String(target.key), VersionId: aws.String(target.version)}); err != nil {
			return err
		}
	}
	final, err := b.client.ListObjectVersions(ctx, &awss3.ListObjectVersionsInput{Bucket: aws.String(b.config.Bucket), Prefix: aws.String(b.config.Namespace.String()), MaxKeys: aws.Int32(1)})
	if err != nil {
		return err
	}
	if final == nil || len(final.Versions)+len(final.DeleteMarkers) > 0 || aws.ToBool(final.IsTruncated) {
		return errors.New("test-owned versions remain")
	}
	return nil
}
