package s3_test

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cloudcredentials "github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

// This controlled HTTP peer exercises the real SDK wire behavior and injected
// failures. It is intentionally not called an S3/R2 provider certification.
type uploadPeer struct {
	failCreate                                     bool
	mu                                             sync.Mutex
	puts, creates, parts, completes, aborts, heads int
	failParts                                      int
	failComplete, failAbort, failHead              bool
	active                                         bool
	published                                      int64
	partSizes                                      map[string]int64
	metadata                                       http.Header
	invalidMD5                                     bool
}

func (p *uploadPeer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	q := r.URL.Query()
	reject := func(status int, code string) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		fmt.Fprintf(w, "<Error><Code>%s</Code><Message>private provider detail</Message></Error>", code)
	}
	consume := func() int64 {
		hash := md5.New()
		n, err := io.Copy(hash, r.Body)
		if err != nil {
			p.invalidMD5 = true
		}
		if base64.StdEncoding.EncodeToString(hash.Sum(nil)) != r.Header.Get("Content-MD5") {
			p.invalidMD5 = true
		}
		return n
	}
	switch {
	case r.Method == "POST" && q.Has("uploads"):
		p.creates++
		p.active = true
		if p.failCreate {
			reject(503, "InternalError")
			return
		}
		p.metadata = r.Header.Clone()
		p.partSizes = make(map[string]int64)
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>fixture-bucket</Bucket><Key>object</Key><UploadId>fixture-upload</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == "GET" && q.Has("uploadId"):
		if !p.active {
			reject(404, "NoSuchUpload")
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<ListPartsResult><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><ETag>"part"</ETag><Size>1</Size></Part></ListPartsResult>`)
	case r.Method == "PUT" && q.Has("uploadId"):
		p.parts++
		size := consume()
		if p.failParts > 0 {
			p.failParts--
			reject(503, "SlowDown")
			return
		}
		p.partSizes[q.Get("partNumber")] = size
		w.Header().Set("ETag", `"part"`)
	case r.Method == "POST" && q.Has("uploadId"):
		p.completes++
		_, _ = io.Copy(io.Discard, r.Body)
		if p.failComplete {
			reject(503, "InternalError")
			return
		}
		p.active = false
		p.published = 0
		for _, size := range p.partSizes {
			p.published += size
		}
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>fixture-bucket</Bucket><Key>object</Key><ETag>"published"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == "DELETE" && q.Has("uploadId"):
		p.aborts++
		if p.failAbort {
			reject(503, "InternalError")
			return
		}
		p.active = false
		w.WriteHeader(204)
	case r.Method == "PUT":
		p.puts++
		p.published = consume()
		p.metadata = r.Header.Clone()
		w.Header().Set("ETag", `"published"`)
	case r.Method == "HEAD":
		p.heads++
		if p.failHead {
			reject(403, "AccessDenied")
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(p.published, 10))
		w.Header().Set("Content-Type", p.metadata.Get("Content-Type"))
		w.Header().Set("ETag", `"published"`)
		w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		for name, values := range p.metadata {
			if strings.HasPrefix(strings.ToLower(name), "x-amz-meta-") {
				for _, value := range values {
					w.Header().Add(name, value)
				}
			}
		}
	default:
		reject(400, "UnexpectedFixtureOperation")
	}
}
func uploadDisk(t *testing.T, peer *uploadPeer) *storage.Disk {
	disk, _ := wireDisk(t, peer, nil)
	return disk
}
func wireDisk(t *testing.T, handler http.Handler, configure func(*s3.Config)) (*storage.Disk, *s3.Backend) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	config := s3.DefaultConfig("fixture-bucket", "us-east-1")
	config.Endpoint = server.URL
	config.PathStyle = true
	config = config.WithCredentials(cloudcredentials.ProviderFunc(func(context.Context) (cloudcredentials.Value, error) {
		return cloudcredentials.Value{AccessKey: secret.New("fixture-key"), SecretKey: secret.New("fixture-secret")}, nil
	}))
	config.HTTPClient = server.Client()
	config.PartBytes = s3.MinPartBytes
	config.MaxUploads = 1
	if configure != nil {
		configure(&config)
	}
	backend, err := s3.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := storage.NewDisk("cloud", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	return disk, backend
}

func objectKey(t *testing.T) storage.ObjectKey {
	t.Helper()
	key, err := storage.ParseKey("object")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

type zeroSource struct{ remaining, read int64 }

func (r *zeroSource) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), r.remaining))
	clear(p[:n])
	r.remaining -= int64(n)
	r.read += int64(n)
	return n, nil
}

type failedSource struct{ cancel context.CancelFunc }

func (r failedSource) Read([]byte) (int, error) {
	if r.cancel != nil {
		r.cancel()
	}
	return 0, errors.New("source interrupted")
}

func TestSDKSmallAndMultipartUploadsConsumeOnceAndPublishMetadata(t *testing.T) {
	for _, size := range []int64{0, 3, s3.MinPartBytes, s3.MinPartBytes + 17} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			peer := &uploadPeer{failParts: 1}
			disk := uploadDisk(t, peer)
			source := &zeroSource{remaining: size}
			result, err := disk.Put(t.Context(), objectKey(t), source, storage.PutOptions{ContentType: "application/octet-stream"})
			if err != nil || result.Object.Size != size || !result.Object.Checksum.IsSet() {
				t.Fatal("published metadata", result, err)
			}
			peer.mu.Lock()
			defer peer.mu.Unlock()
			if source.read != size || peer.invalidMD5 || peer.published != size || peer.aborts != 0 {
				t.Fatal("transfer integrity or ownership changed")
			}
			if size <= s3.MinPartBytes {
				if peer.puts != 1 || peer.creates != 0 {
					t.Fatal("small upload started multipart")
				}
			} else if peer.puts != 0 || peer.creates != 1 || peer.parts != 3 || peer.completes != 1 {
				t.Fatal("multipart retry was not limited to replayable parts", peer.parts, peer.completes)
			}
		})
	}
}
func TestMultipartFailuresAbortWithIndependentContextAndPreserveOutcomes(t *testing.T) {
	for _, mode := range []string{"source", "cancel", "parts", "complete", "abort", "post-head"} {
		t.Run(mode, func(t *testing.T) {
			peer := &uploadPeer{failParts: 0, failComplete: mode == "complete", failAbort: mode == "abort", failHead: mode == "post-head"}
			if mode == "parts" {
				peer.failParts = 10
			}
			disk := uploadDisk(t, peer)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			data := &zeroSource{remaining: s3.MinPartBytes + 17}
			var source io.Reader = data
			if mode == "source" || mode == "cancel" || mode == "abort" {
				failed := failedSource{}
				if mode == "cancel" {
					failed.cancel = cancel
				}
				source = io.MultiReader(source, failed)
			}
			result, err := disk.Put(ctx, objectKey(t), source, storage.PutOptions{})
			if mode == "post-head" {
				// A refused verification read (write-only credentials) leaves the
				// acknowledged publication applied and unverified, not uncertain.
				peer.mu.Lock()
				defer peer.mu.Unlock()
				if err != nil || result.Object.ETag != `"published"` || result.Object.Modified.IsZero() || peer.aborts != 0 || peer.published == 0 {
					t.Fatal("acknowledged publication was reported as failed", err)
				}
				return
			}
			var failure *storage.Error
			if err == nil || !result.Object.Key.IsZero() || !errors.As(err, &failure) {
				t.Fatal("failed upload returned publication", result, err)
			}
			expected := storage.Unchanged
			if mode == "complete" {
				expected = storage.Unknown
			}
			if failure.Outcome() != expected {
				t.Fatal("mutation outcome", failure.Outcome(), expected, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation disappeared", err)
			}
			if mode == "abort" && !failure.Cleanup().IsSet() {
				t.Fatal("abort failure lost reconciliation reference")
			}
			if strings.Contains(fmt.Sprintf("%+v", err), "private provider") {
				t.Fatal("provider response leaked")
			}
			peer.mu.Lock()
			defer peer.mu.Unlock()
			if peer.aborts == 0 {
				t.Fatal("multipart state was abandoned")
			}
			if mode == "complete" && peer.completes != 1 {
				t.Fatal("ambiguous complete was retried")
			}
			if mode == "parts" && peer.parts != 3 {
				t.Fatal("part retry bound changed", peer.parts)
			}
			if mode != "abort" && peer.active {
				t.Fatal("upload left active parts")
			}
		})
	}
}

func TestLostMultipartCreationReportsInspectableScopeWithoutRetry(t *testing.T) {
	peer := &uploadPeer{failCreate: true}
	disk, backend := wireDisk(t, peer, nil)
	_, err := disk.Put(t.Context(), objectKey(t), &zeroSource{remaining: s3.MinPartBytes + 1}, storage.PutOptions{})
	var failure *storage.Error
	if !errors.As(err, &failure) || failure.Outcome() != storage.Unchanged {
		t.Fatal("creation changed object outcome", err)
	}
	cleanup, ok := failure.Cleanup().Get()
	if !ok {
		t.Fatal("lost creation response has no inspection scope")
	}
	if _, err := backend.UploadFromCleanup(cleanup); !errors.Is(err, storage.Unsupported) {
		t.Fatal("unknown upload ID was invented", err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.creates != 1 || peer.aborts != 0 || peer.completes != 0 {
		t.Fatal("ambiguous creation was retried or guessed")
	}
}

type pausedInput struct {
	entered, resume chan struct{}
	once            sync.Once
}

func (r *pausedInput) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.resume
	return 0, io.EOF
}
func TestSharedUploadAdmissionRemainsOwnedUntilBlockedSourceExits(t *testing.T) {
	peer := &uploadPeer{}
	disk, backend := wireDisk(t, peer, nil)
	other, err := storage.NewDisk("other", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(context.Background())
	input := &pausedInput{entered: make(chan struct{}), resume: make(chan struct{})}
	var once sync.Once
	resume := func() { once.Do(func() { close(input.resume) }) }
	defer resume()
	done := make(chan error, 1)
	go func() { _, err := disk.Put(t.Context(), objectKey(t), input, storage.PutOptions{}); done <- err }()
	<-input.entered
	busy, cancelBusy := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelBusy()
	if _, err := other.PutBytes(busy, objectKey(t), []byte("capacity"), storage.PutOptions{}); !errors.Is(err, fault.Overloaded) {
		t.Fatal("shared upload budget ignored", err)
	}
	stop, cancel := context.WithCancel(context.Background())
	cancel()
	if err := disk.Close(stop); !errors.Is(err, context.Canceled) {
		t.Fatal("blocked source was abandoned", err)
	}
	if disk.Stats().Active != 1 {
		t.Fatal("blocked source released capacity early")
	}
	select {
	case <-disk.Done():
		t.Fatal("disk completed before source exited")
	default:
	}
	resume()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := disk.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := other.PutBytes(t.Context(), objectKey(t), []byte("reused"), storage.PutOptions{}); err != nil {
		t.Fatal("shared upload slot leaked", err)
	}
}
