package s3

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// This test-only adapter reaches the real R2 publication protocol while the
// shipping profile remains gated. It is not a consumer opt-out or certification.
// Every other R2 capability and its actual SDK client remain unchanged.
type conditionalCandidate struct{ *Backend }

func (b conditionalCandidate) Capabilities() storage.Capabilities {
	c := b.Backend.Capabilities()
	c.ConditionalWriteMaxBytes = 0
	return c
}
func (b conditionalCandidate) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	if err := b.ready(ctx, storage.PutOperation); err != nil {
		return storage.ObjectInfo{}, err
	}
	full, err := b.object(key)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if source == nil {
		return storage.ObjectInfo{}, storage.Invalid
	}
	if err := options.Validate(b.config.MaxObjectBytes); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.Capabilities().ValidatePut(options); err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := b.validateMetadata(options.Metadata); err != nil {
		return storage.ObjectInfo{}, err
	}
	return b.put(ctx, key, full, source, options)
}
func candidateDisk(t *testing.T, b *Backend) *storage.Disk {
	t.Helper()
	disk, err := storage.NewDisk("conditional-candidate", conditionalCandidate{b}, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return disk
}

type conditionalGate struct {
	client                                        aws.HTTPClient
	mu                                            sync.Mutex
	remaining                                     int
	arrived                                       chan struct{}
	release                                       chan struct{}
	lostComplete, lostCreate, failPart, failAbort bool
	terminals, creates, aborts                    int
}

func (g *conditionalGate) hold(count int) (<-chan struct{}, chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.remaining = count
	g.arrived = make(chan struct{}, count)
	g.release = make(chan struct{})
	return g.arrived, g.release
}
func (g *conditionalGate) Do(r *http.Request) (*http.Response, error) {
	q := r.URL.Query()
	terminal := r.Method == "PUT" && !q.Has("uploadId") || r.Method == "POST" && q.Has("uploadId")
	create := r.Method == "POST" && q.Has("uploads")
	g.mu.Lock()
	var arrived chan struct{}
	var release chan struct{}
	if terminal {
		g.terminals++
		if g.remaining > 0 {
			g.remaining--
			arrived, release = g.arrived, g.release
		}
	}
	if create {
		g.creates++
	}
	abort := r.Method == "DELETE" && q.Has("uploadId")
	if abort {
		g.aborts++
	}
	fail := g.failPart && r.Method == "PUT" && q.Has("uploadId") || g.failAbort && abort
	lost := terminal && g.lostComplete || create && g.lostCreate
	g.mu.Unlock()
	if arrived != nil {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	if fail {
		return &http.Response{StatusCode: 503, Status: "503 Service Unavailable", Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader(`<Error><Code>InternalError</Code></Error>`)), Request: r}, nil
	}
	response, err := g.client.Do(r)
	if lost && err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return nil, io.ErrUnexpectedEOF
	}
	return response, err
}

type conditionalByte byte

func (b conditionalByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
func conditionalOptions(size int64, content byte, condition storage.WriteCondition) storage.PutOptions {
	hash := sha256.New()
	_, _ = io.Copy(hash, io.LimitReader(conditionalByte(content), size))
	var digest storage.SHA256
	copy(digest[:], hash.Sum(nil))
	return storage.PutOptions{Size: value.Set(size), Checksum: value.Set(digest), ContentType: storage.Binary, Condition: condition}
}
func conditionalPut(ctx context.Context, disk *storage.Disk, key storage.ObjectKey, size int64, content byte, condition storage.WriteCondition) (storage.StoredObject, error) {
	return disk.Put(ctx, key, io.LimitReader(conditionalByte(content), size), conditionalOptions(size, content, condition))
}
func conditionalRead(t *testing.T, disk *storage.Disk, key storage.ObjectKey, size int64, content byte) {
	t.Helper()
	reader, info, err := disk.Open(t.Context(), key, storage.ReadOptions{})
	if err != nil {
		t.Fatal("candidate read", err)
	}
	hash := sha256.New()
	n, readErr := io.Copy(hash, reader)
	closeErr := reader.Close()
	want, _ := conditionalOptions(size, content, storage.WriteCondition{}).Checksum.Get()
	var got storage.SHA256
	copy(got[:], hash.Sum(nil))
	if readErr != nil || closeErr != nil || n != size || info.Object.Size != size || got != want {
		t.Fatal("conditional mutation changed winning bytes", readErr, closeErr)
	}
}
func conditionalFailure(t *testing.T, err error, outcome storage.Outcome) {
	t.Helper()
	var detail *storage.Error
	if !errors.As(err, &detail) || detail.Outcome() != outcome {
		t.Fatal("incorrect publication outcome", err)
	}
}
func conditionFor(t *testing.T, tag storage.ETag) storage.WriteCondition {
	t.Helper()
	c, err := storage.IfMatch(tag)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func waitConditional(t *testing.T, arrived <-chan struct{}, count int) {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for range count {
		select {
		case <-arrived:
		case <-timer.C:
			t.Fatal("publication did not reach terminal request")
		}
	}
}

// The same cases run on the SDK wire peer and the explicitly opted-in live R2
// namespace. Concurrent requests are held immediately before Put/Complete, so
// the server must evaluate conditions at final publication, not initiation.
func runConditionalMatrix(t *testing.T, disk *storage.Disk, b *Backend, gate *conditionalGate, size int64) {
	keyFor := func(name string) storage.ObjectKey {
		key, err := storage.ParseKey(name)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	put := func(t *testing.T, key storage.ObjectKey, content byte, c storage.WriteCondition) storage.StoredObject {
		r, err := conditionalPut(t.Context(), disk, key, size, content, c)
		if err != nil {
			t.Fatal("conditional publication", err)
		}
		return r
	}
	t.Run("absent-existing-and-matching-stale-missing", func(t *testing.T) {
		key := keyFor("sequential")
		first := put(t, key, 'a', storage.IfAbsent())
		_, err := conditionalPut(t.Context(), disk, key, size, 'b', storage.IfAbsent())
		if !errors.Is(err, storage.PreconditionFailed) {
			t.Fatal("existing create succeeded", err)
		}
		conditionalFailure(t, err, storage.Unchanged)
		second := put(t, key, 'b', conditionFor(t, first.Object.ETag))
		if second.Object.ETag == first.Object.ETag {
			t.Fatal("different representations share fixture ETag")
		}
		_, err = conditionalPut(t.Context(), disk, key, size, 'c', conditionFor(t, first.Object.ETag))
		if !errors.Is(err, storage.PreconditionFailed) {
			t.Fatal("stale replace succeeded", err)
		}
		conditionalFailure(t, err, storage.Unchanged)
		_, err = conditionalPut(t.Context(), disk, keyFor("missing"), size, 'c', conditionFor(t, first.Object.ETag))
		if !errors.Is(err, storage.PreconditionFailed) && !errors.Is(err, storage.NotFound) {
			t.Fatal("missing replace unexpectedly accepted", err)
		}
		conditionalFailure(t, err, storage.Unchanged)
		conditionalRead(t, disk, key, size, 'b')
	})
	for _, replace := range []bool{false, true} {
		name := "create"
		if replace {
			name = "replace"
		}
		t.Run("competing-"+name, func(t *testing.T) {
			key := keyFor("competing-" + name)
			condition := storage.IfAbsent()
			if replace {
				old := put(t, key, 'a', condition)
				condition = conditionFor(t, old.Object.ETag)
			}
			arrived, release := gate.hold(2)
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			type outcome struct {
				content byte
				err     error
			}
			results := make(chan outcome, 2)
			for _, content := range []byte{'b', 'c'} {
				go func() {
					_, err := conditionalPut(t.Context(), disk, key, size, content, condition)
					results <- outcome{content, err}
				}()
			}
			waitConditional(t, arrived, 2)
			close(release)
			wins := 0
			var winner byte
			for range 2 {
				r := <-results
				if r.err == nil {
					wins++
					winner = r.content
				} else {
					if !errors.Is(r.err, storage.PreconditionFailed) {
						t.Fatal("competing write failed unexpectedly", r.err)
					}
					conditionalFailure(t, r.err, storage.Unchanged)
				}
			}
			if wins != 1 {
				t.Fatal("conditional competition did not have exactly one winner", wins)
			}
			conditionalRead(t, disk, key, size, winner)
		})
		t.Run("destination-changes-before-"+name+"-completion", func(t *testing.T) {
			key := keyFor("changed-" + name)
			condition := storage.IfAbsent()
			if replace {
				old := put(t, key, 'a', condition)
				condition = conditionFor(t, old.Object.ETag)
			}
			arrived, release := gate.hold(1)
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			done := make(chan error, 1)
			go func() { _, err := conditionalPut(t.Context(), disk, key, size, 'b', condition); done <- err }()
			waitConditional(t, arrived, 1)
			put(t, key, 'c', storage.WriteCondition{})
			close(release)
			err := <-done
			if !errors.Is(err, storage.PreconditionFailed) {
				t.Fatal("condition evaluated before completion", err)
			}
			conditionalFailure(t, err, storage.Unchanged)
			conditionalRead(t, disk, key, size, 'c')
		})
		t.Run("lost-"+name+"-acknowledgement", func(t *testing.T) {
			key := keyFor("lost-" + name)
			condition := storage.IfAbsent()
			if replace {
				old := put(t, key, 'a', condition)
				condition = conditionFor(t, old.Object.ETag)
			}
			gate.mu.Lock()
			before := gate.terminals
			gate.lostComplete = true
			gate.mu.Unlock()
			_, err := conditionalPut(t.Context(), disk, key, size, 'b', condition)
			gate.mu.Lock()
			gate.lostComplete = false
			attempts := gate.terminals - before
			gate.mu.Unlock()
			conditionalFailure(t, err, storage.Unknown)
			if attempts != 1 {
				t.Fatal("ambiguous completion retried", attempts)
			}
			conditionalRead(t, disk, key, size, 'b') // A successful staging abort must not erase this object.
		})
		if size > b.config.PartBytes {
			t.Run(name+"-cancellation", func(t *testing.T) {
				key := keyFor(name + "-cancel")
				condition := storage.IfAbsent()
				if replace {
					old := put(t, key, 'a', condition)
					condition = conditionFor(t, old.Object.ETag)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				source := io.MultiReader(io.LimitReader(conditionalByte('b'), b.config.PartBytes+1), conditionalCancelReader{cancel})
				_, err := disk.Put(ctx, key, source, conditionalOptions(size, 'b', condition))
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
				conditionalFailure(t, err, storage.Unchanged)
				if replace {
					conditionalRead(t, disk, key, size, 'a')
				} else if exists, err := disk.Exists(t.Context(), key); err != nil || exists {
					t.Fatal("cancellation published", err)
				}
			})
			t.Run(name+"-lost-upload-id", func(t *testing.T) {
				key := keyFor(name + "-lost-id")
				condition := storage.IfAbsent()
				if replace {
					old := put(t, key, 'a', condition)
					condition = conditionFor(t, old.Object.ETag)
				}
				gate.mu.Lock()
				gate.lostCreate = true
				before := gate.creates
				gate.mu.Unlock()
				_, err := conditionalPut(t.Context(), disk, key, size, 'b', condition)
				gate.mu.Lock()
				gate.lostCreate = false
				attempts := gate.creates - before
				gate.mu.Unlock()
				conditionalFailure(t, err, storage.Unchanged)
				var detail *storage.Error
				errors.As(err, &detail)
				ref, ok := detail.Cleanup().Get()
				if !ok || attempts != 1 {
					t.Fatal("lost initiation was retried or lost inspection identity", err)
				}
				if _, err := b.UploadFromCleanup(ref); !errors.Is(err, storage.Unsupported) {
					t.Fatal("missing upload ID presented as abortable", err)
				}
				page, err := b.ListUploads(t.Context(), storage.ListOptions{Limit: 100})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, pending := range page.Uploads {
					if pending.Reference.Key() == key {
						found = true
						if err := b.AbortUpload(t.Context(), pending.Reference); err != nil {
							t.Fatal(err)
						}
					}
				}
				if !found {
					t.Fatal("lost upload ID is not inspectable")
				}
				if replace {
					conditionalRead(t, disk, key, size, 'a')
				}
			})
			for _, failAbort := range []bool{false, true} {
				label := "abort"
				if failAbort {
					label = "failed-abort"
				}
				t.Run(name+"-part-failure-"+label, func(t *testing.T) {
					key := keyFor(name + label)
					condition := storage.IfAbsent()
					if replace {
						old := put(t, key, 'a', condition)
						condition = conditionFor(t, old.Object.ETag)
					}
					gate.mu.Lock()
					gate.failPart = true
					gate.failAbort = failAbort
					before := gate.aborts
					gate.mu.Unlock()
					_, err := conditionalPut(t.Context(), disk, key, size, 'b', condition)
					gate.mu.Lock()
					gate.failPart = false
					gate.failAbort = false
					aborts := gate.aborts - before
					gate.mu.Unlock()
					conditionalFailure(t, err, storage.Unchanged)
					if aborts == 0 {
						t.Fatal("failed multipart never attempted abort")
					}
					var detail *storage.Error
					errors.As(err, &detail)
					if failAbort {
						ref, ok := detail.Cleanup().Get()
						if !ok {
							t.Fatal("failed abort lost cleanup identity")
						}
						upload, err := b.UploadFromCleanup(ref)
						if err != nil {
							t.Fatal(err)
						}
						if err := b.AbortUpload(t.Context(), upload); err != nil {
							t.Fatal("tracked staging cannot be reclaimed", err)
						}
					} else if detail.Cleanup().IsSet() {
						t.Fatal("successful abort left a cleanup debt")
					}
					if replace {
						conditionalRead(t, disk, key, size, 'a')
					} else if exists, err := disk.Exists(t.Context(), key); err != nil || exists {
						t.Fatal("failed multipart created destination", err)
					}
				})
			}
		}
	}
	pending, err := b.ListUploads(t.Context(), storage.ListOptions{Limit: 100})
	if err != nil || len(pending.Uploads) != 0 || !pending.Next.IsZero() {
		t.Fatal("matrix left untracked staging", err)
	}
}

func TestRealR2ConditionalMultipartCandidate(t *testing.T) {
	// realConfig requires explicit test bucket/prefix and owner-supplied credentials.
	// This test never changes the production capability restriction.
	cfg := realConfig(t, R2)
	cfg.MaxUploads = 4
	cfg.PartAttempts = 1
	gate := &conditionalGate{client: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	cfg.HTTPClient = gate
	for _, size := range []int64{cfg.PartBytes, cfg.PartBytes + 1} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			_, backend := providerFactory(t, cfg)
			disk := candidateDisk(t, backend)
			runConditionalMatrix(t, disk, backend, gate, size)
		})
	}
}

type conditionalCancelReader struct{ cancel context.CancelFunc }

func (r conditionalCancelReader) Read([]byte) (int, error) { r.cancel(); return 0, context.Canceled }
