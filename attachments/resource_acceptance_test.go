package attachments

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

const resourcePrefix = "B26_RESOURCE "

// Each cell has its own process, including its own allocator high-water mark.
// The remote generator runs in another process and never retains the payload.
func TestB26ResourceAcceptance(t *testing.T) {
	if os.Getenv("FOUNDRY_TEST_ATTACHMENT_RESOURCES") != "1" {
		t.Skip("set FOUNDRY_TEST_ATTACHMENT_RESOURCES=1; requires isolated PostgreSQL")
	}
	if os.Getenv("FOUNDRY_TEST_POSTGRES_REQUIRED") != "1" {
		t.Fatal("resource acceptance requires FOUNDRY_TEST_POSTGRES_REQUIRED=1, never a skipped database")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"prepare", "http", "accepts", "accepts-rejected", "url", "full-read", "small-range", "fallback", "settle", "historical-variant", "url:limit", "url:error", "url:cancel", "accepts:cancel", "fallback:mismatch", "fallback:cancel", "settle:mismatch", "settle:cancel", "full-read:corrupt", "full-read:short"}
	for _, concurrency := range []int{1, 2} {
		for _, path := range paths {
			var previous resourceReport
			for _, size := range []int64{128 << 20, 512 << 20} {
				var report resourceReport
				t.Run(fmt.Sprintf("%s/%d/%d", path, size, concurrency), func(t *testing.T) { report = runResourceCell(t, executable, path, size, concurrency) })
				if report.Size == 0 {
					continue
				}
				if previous.Size != 0 && report.HeapDelta > previous.HeapDelta+uint64(concurrency)*(16<<20) {
					t.Errorf("%s concurrency=%d live heap scales with content: %d -> %d", path, concurrency, previous.HeapDelta, report.HeapDelta)
				}
				if previous.Size != 0 && report.Allocated > previous.Allocated+uint64(concurrency)*(16<<20) {
					t.Errorf("%s concurrency=%d allocation scales with content: %d -> %d", path, concurrency, previous.Allocated, report.Allocated)
				}
				previous = report
			}
		}
		for _, path := range []string{"prepare", "accepts", "url"} {
			for _, size := range []int64{MaxBufferedBytes - 1, MaxBufferedBytes, MaxBufferedBytes + 1} {
				t.Run(fmt.Sprintf("legacy/%s/%d/%d", path, size, concurrency), func(t *testing.T) { runResourceCell(t, executable, path, size, concurrency) })
			}
		}
	}
}

type resourceReport struct {
	Path                                             string `json:"path"`
	Size                                             int64  `json:"size"`
	Concurrency                                      int    `json:"concurrency"`
	Go, OS, Arch                                     string
	GOMAXPROCS                                       int
	GOGC, GOMEMLIMIT                                 string
	BaselineHeap, PeakHeap, HeapDelta, Allocated     uint64
	BaselineRSS, PeakRSS                             int64
	RequestDisk, AttachmentDisk, LocalStagingDisk    int64
	SourceBytes, StorageReadBytes, StorageWriteBytes int64
	Digest                                           string
	Disk                                             storage.Config
	PolicyBytes, RequestBytes                        int64
	GCPercent, MemoryLimit                           uint64
	SampleCount                                      int
	// This matrix uses the real local adapter. The separate S3 wire/provider gate
	// records multipart settings; zero here must not be mistaken for S3 evidence.
	PartBytes, PartConcurrency                      int
	ManagerMaxActive, ManagerMaxReads, RequestSlots int
}

func runResourceCell(t *testing.T, executable, path string, size int64, concurrency int) resourceReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestB26ResourceCell$", "-test.v", "-test.timeout=7m")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = append(os.Environ(), "FOUNDRY_RESOURCE_CELL="+path, "FOUNDRY_RESOURCE_SIZE="+strconv.FormatInt(size, 10), "FOUNDRY_RESOURCE_CONCURRENCY="+strconv.Itoa(concurrency))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resource cell %s/%d/%d failed: %v\n%s", path, size, concurrency, err, output)
	}
	var report resourceReport
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, resourcePrefix) {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, resourcePrefix)), &report); err != nil {
				t.Fatal(err)
			}
			t.Log(line)
		}
	}
	if report.Size != size || report.Path != path || report.Concurrency != concurrency || report.SampleCount == 0 {
		t.Fatal("missing resource evidence", path, size, concurrency)
	}
	return report
}

func TestB26ResourceCell(t *testing.T) {
	path := os.Getenv("FOUNDRY_RESOURCE_CELL")
	if path == "" {
		t.Skip("subprocess only")
	}
	size, err := strconv.ParseInt(os.Getenv("FOUNDRY_RESOURCE_SIZE"), 10, 64)
	if err != nil || size <= 0 || size > 512<<20 {
		t.Fatal("invalid resource size")
	}
	concurrency, err := strconv.Atoi(os.Getenv("FOUNDRY_RESOURCE_CONCURRENCY"))
	if err != nil || concurrency < 1 || concurrency > 2 {
		t.Fatal("invalid resource concurrency")
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(repeatedByte('x'), size)); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", h.Sum(nil))
	var workers []*resourceWorker
	for i := 0; i < concurrency; i++ {
		workers = append(workers, newResourceWorker(t, path, size, digest))
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	report := resourceReport{Path: path, Size: size, Concurrency: concurrency, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0), GOGC: os.Getenv("GOGC"), GOMEMLIMIT: os.Getenv("GOMEMLIMIT"), BaselineHeap: before.HeapAlloc, PeakHeap: before.HeapAlloc, BaselineRSS: resourceRSS(t), Digest: digest, ManagerMaxActive: 1, ManagerMaxReads: 1, RequestSlots: 1}
	runtimeSettings := []metrics.Sample{{Name: "/gc/gogc:percent"}, {Name: "/gc/gomemlimit:bytes"}}
	metrics.Read(runtimeSettings)
	report.GCPercent, report.MemoryLimit = runtimeSettings[0].Value.Uint64(), runtimeSettings[1].Value.Uint64()
	report.PeakRSS = report.BaselineRSS
	report.Disk = workers[0].f.disk.Config()
	report.PolicyBytes = workers[0].c.definition.policy.MaxBytes
	if path == "http" {
		report.RequestBytes = 513 << 20
	} else {
		report.RequestSlots = 0
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(10 * time.Millisecond)
		defer timer.Stop()
		for {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			report.PeakHeap = max(report.PeakHeap, memory.HeapAlloc)
			if report.SampleCount%5 == 0 {
				report.PeakRSS = max(report.PeakRSS, resourceRSS(t))
			}
			var request, attachment, staging int64
			for _, w := range workers {
				request += resourceDisk(t, w.request, false)
				attachment += resourceDisk(t, w.f.manager.config.TempDirectory, false)
				staging += resourceDisk(t, w.f.localRoot, true)
			}
			report.RequestDisk = max(report.RequestDisk, request)
			report.AttachmentDisk = max(report.AttachmentDisk, attachment)
			report.LocalStagingDisk = max(report.LocalStagingDisk, staging)
			report.SampleCount++
			select {
			case <-stop:
				return
			case <-timer.C:
			}
		}
	}()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, w := range workers {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; w.run(t) }()
	}
	close(start)
	wg.Wait()
	runtime.ReadMemStats(&after)
	close(stop)
	<-done
	report.PeakHeap = max(report.PeakHeap, after.HeapAlloc)
	report.HeapDelta = report.PeakHeap - report.BaselineHeap
	report.Allocated = after.TotalAlloc - before.TotalAlloc
	for _, w := range workers {
		report.SourceBytes += w.source.Load()
		report.StorageReadBytes += w.probe.bytes.Load()
		report.StorageWriteBytes += w.written.Load()
		w.check(t)
	}
	if report.HeapDelta > uint64(concurrency)*(96<<20) || report.Allocated > uint64(concurrency)*(96<<20) {
		t.Errorf("content-sized allocation: heap=%d cumulative=%d budget=%d", report.HeapDelta, report.Allocated, concurrency*(96<<20))
	}
	if report.RequestDisk > int64(concurrency)*(size+1<<20) || report.AttachmentDisk > int64(concurrency)*size || report.LocalStagingDisk > int64(concurrency)*(size+1<<20) {
		t.Error("spool exceeded admitted disk bound")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(resourcePrefix + string(encoded))
}

func resourceRSS(t *testing.T) int64 {
	t.Helper()
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/self/statm")
		if err != nil {
			t.Error(err)
			return 0
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			t.Error("missing RSS")
			return 0
		}
		pages, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			t.Error(err)
		}
		return pages * int64(os.Getpagesize())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Error(err)
		return 0
	}
	kib, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		t.Error(err)
	}
	return kib * 1024
}

func resourceDisk(t *testing.T, root string, stagingOnly bool) int64 {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() || stagingOnly && !strings.HasPrefix(entry.Name(), ".foundry-tmp-") {
			return nil
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	return total
}

type resourceWorker struct {
	path            string
	size            int64
	digest          string
	f               attachmentFixture
	c               Collection[extensiontest.Member, int64]
	probe           *recoveryProbe
	source, written atomic.Int64
	request         string
	result          Result[extensiontest.Member, int64]
	run             func(*testing.T)
	check           func(*testing.T)
}

type resourceInput struct {
	size   int64
	count  *atomic.Int64
	cancel context.CancelFunc
}

func (r resourceInput) Name() string              { return "payload.txt" }
func (r resourceInput) ClientContentType() string { return "text/plain" }
func (r resourceInput) Size() int64               { return r.size }
func (r resourceInput) Open(context.Context) (io.ReadSeekCloser, error) {
	return &resourceReader{size: r.size, count: r.count, cancel: r.cancel}, nil
}

type resourceReader struct {
	size, offset int64
	count        *atomic.Int64
	cancel       context.CancelFunc
}

func (r *resourceReader) Read(p []byte) (int, error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), r.size-r.offset))
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.offset += int64(n)
	r.count.Add(int64(n))
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return n, nil
}
func (r *resourceReader) Seek(n int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.offset = n
	case io.SeekCurrent:
		r.offset += n
	case io.SeekEnd:
		r.offset = r.size + n
	default:
		return 0, errors.New("invalid whence")
	}
	return r.offset, nil
}
func (*resourceReader) Close() error { return nil }

type resourceCountReader struct {
	io.Reader
	count *atomic.Int64
}

func (r resourceCountReader) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	r.count.Add(int64(n))
	return n, e
}

func newResourceWorker(t *testing.T, path string, size int64, digest string) *resourceWorker {
	path, mode, _ := strings.Cut(path, ":")
	w := &resourceWorker{path: path, size: size, digest: digest, request: t.TempDir()}
	var faultBackend *resourceFaultBackend
	policy := largePolicy()
	if mode == "limit" {
		policy.MaxBytes, policy.MaxStoredBytes = size, size
	}
	if path == "accepts-rejected" {
		policy.AnyMedia = false
		policy.Accepted = []storage.MediaType{"image/png"}
	}
	w.c = Define(extensiontest.Members, "resource", policy)
	w.f = openAttachmentsOn(t, nil, func(inner storage.Backend) storage.Backend {
		faultBackend = &resourceFaultBackend{Backend: inner}
		w.probe = &recoveryProbe{Backend: faultBackend, omitChecksum: path == "fallback"}
		return w.probe
	}, w.c.Registration())
	if err := w.f.manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.MaxActive, config.MaxReads = 1, 1
	config.Timeout = 5 * time.Minute
	config.TempDirectory = t.TempDir()
	var err error
	w.f.manager, err = New(Dependencies{Store: w.f.Store, Disks: w.f.registry, Image: w.f.image}, config, w.c.Registration())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.f.manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	input := func() Upload {
		return Upload{Source: &resourceReader{size: size, count: &w.source}, OriginalName: "payload.txt", ContentType: "text/plain"}
	}
	w.f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, body io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		info, err := w.f.backend.Backend.Put(ctx, key, resourceCountReader{body, &w.written}, options)
		if err == nil && (path == "fallback" || path == "settle") {
			return storage.ObjectInfo{}, storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil)
		}
		return info, err
	})
	owner := member(t, 1)
	publish := func(t *testing.T, upload Upload) {
		prepared, err := w.c.Prepare(t.Context(), w.f.manager, owner, upload)
		if err != nil {
			t.Fatal(err)
		}
		err = w.f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			var err error
			w.result, err = w.c.ReplaceIn(ctx, tx, w.f.manager, owner, prepared)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	w.check = func(t *testing.T) {
		assertSpoolEmpty(t, w.f.manager.config.TempDirectory)
		if w.f.disk.Stats().Streams != 0 {
			t.Error("retained storage reader")
		}
		if resourceDisk(t, w.f.localRoot, true) != 0 {
			t.Error("retained local staging")
		}

		if mode != "" {
			if path == "url" || path == "accepts" {
				if w.written.Load() != 0 {
					t.Error("rejected source reached publication")
				}
				files, err := w.c.List(t.Context(), w.f.manager, owner)
				if err != nil || len(files) != 0 {
					t.Error("rejected source published membership", err)
				}
			} else if path == "fallback" || path == "settle" {
				if journal(t, w.f, w.result.Operation).State != string(Uncertain) {
					t.Error("incomplete evidence resolved intent")
				}
				row := journal(t, w.f, w.result.Operation)
				key, err := storage.ParseKey(row.ObjectKey)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.f.disk.Stat(t.Context(), key, storage.ReadOptions{}); err != nil {
					t.Error("incomplete evidence removed object", err)
				}
			}
			if mode == "cancel" && path != "url" && (w.source.Load()+w.probe.bytes.Load() == 0 || w.source.Load()+w.probe.bytes.Load() >= size) {
				t.Error("cancellation did not interrupt payload")
			}
			if mode == "mismatch" || mode == "corrupt" {
				if w.probe.bytes.Load() != size {
					t.Error("corrupt full read did not consume exact object")
				}
			}
			if mode == "short" && w.probe.bytes.Load() != size-1 {
				t.Error("truncation payload count")
			}
			return
		}
		switch path {
		case "prepare", "http", "url":
			if w.written.Load() != size || w.source.Load() != size {
				t.Error("publication payload count", w.written.Load())
			}
			file := attachmentOf(t, w.result)
			if file.Info().Size != size || journal(t, w.f, w.result.Operation).Digest != digest {
				t.Error("publication metadata mismatch")
			}
			body, _, err := w.c.Open(t.Context(), w.f.manager, owner, file.ID())
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.New()
			n, err := io.Copy(h, body)
			closeErr := body.Close()
			if err != nil || closeErr != nil || n != size || fmt.Sprintf("%x", h.Sum(nil)) != digest {
				t.Error("stored bytes/digest mismatch", err, closeErr)
			}
		case "accepts", "accepts-rejected":
			if w.written.Load() != 0 || w.source.Load() != size {
				t.Error("Accepts did not count exact input without publication")
			}
			files, err := w.c.List(t.Context(), w.f.manager, owner)
			if err != nil || len(files) != 0 {
				t.Error("Accepts published membership", err)
			}
		case "historical-variant":
			if w.probe.bytes.Load() != 0 || faultBackend.opens.Load() != 0 {
				t.Error("variant rejection opened/read original")
			}
			file := attachmentOf(t, w.result)
			reader, _, err := w.c.Open(t.Context(), w.f.manager, owner, file.ID())
			if err != nil {
				t.Fatal(err)
			}
			n, readErr := io.Copy(io.Discard, reader)
			closeErr := reader.Close()
			if n != size || readErr != nil || closeErr != nil {
				t.Error("historical original stopped streaming", readErr, closeErr)
			}
			if _, err := w.c.Detach(t.Context(), w.f.manager, owner, file.ID()); err != nil {
				t.Error("historical retirement failed", err)
			}
		case "small-range":
			if w.probe.bytes.Load() != 64<<10 {
				t.Error("range read beyond requested span", w.probe.bytes.Load())
			}
		default:
			if w.probe.bytes.Load() != size {
				t.Error("whole-content verification did not read exact size", w.probe.bytes.Load())
			}
		}
	}
	switch path {
	case "prepare":
		w.run = func(t *testing.T) { publish(t, input()) }
	case "http":
		pool, err := foundryhttp.NewUploadPool(foundryhttp.UploadPoolConfig{Directory: w.request, MaxActive: 1, MaxBytes: 513 << 20})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := pool.Close(); err != nil {
				t.Error(err)
			}
		})
		type body struct{ File foundryhttp.UploadedFile }
		limits := foundryhttp.DefaultEndpointLimits()
		limits.Multipart.Bytes = 513 << 20
		limits.Multipart.FileBytes = 512 << 20
		limits.Multipart.Files = 1
		limits.Multipart.Parts = 1
		limits.Multipart.Readers = 1
		form := foundryhttp.MultipartAdmission(foundryhttp.DefineMultipart(foundryhttp.FilePart("file", func(b *body) *foundryhttp.UploadedFile { return &b.File })), pool)
		endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "resource.upload", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/upload"), foundryhttp.BodyLimit(513<<20), foundryhttp.Timeout(5*time.Minute)), foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.EmptyResponse(204), foundryhttp.Limits(limits))
		router, err := foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, body]) (foundryhttp.NoContent, error) {
			reader, err := in.Body.File.Open(ctx)
			if err != nil {
				return foundryhttp.NoContent{}, err
			}
			defer reader.Close()
			publish(t, Upload{Source: reader, OriginalName: "payload.txt"})
			return foundryhttp.NoContent{}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		w.run = func(t *testing.T) {
			request := httptest.NewRequest("POST", "/upload", io.MultiReader(strings.NewReader("--resource\r\nContent-Disposition: form-data; name=\"file\"; filename=\"payload.txt\"\r\nContent-Type: text/plain\r\n\r\n"), input().Source, strings.NewReader("\r\n--resource--\r\n")))
			request.Header.Set("Content-Type", "multipart/form-data; boundary=resource")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 204 {
				t.Fatal("HTTP publication status", response.Code, response.Body.String())
			}
			if active, bytes := pool.Usage(); active != 0 || bytes != 0 {
				t.Error("request reservation retained")
			}
		}
	case "accepts", "accepts-rejected":
		slot := slotDefinition[extensiontest.Member, int64, One[extensiontest.Member]]{collection: w.c, manager: w.f.manager, binding: extensions.SlotBinding[extensiontest.Member, int64, One[extensiontest.Member]]{Field: "File", Reference: func(m extensiontest.Member) model.Reference[extensiontest.Member, int64] {
			return extensions.ORMIdentity(extensiontest.Members).Reference(m.ID)
		}, Get: func(extensiontest.Member) One[extensiontest.Member] { return One[extensiontest.Member]{} }, Set: func(m extensiontest.Member, _ One[extensiontest.Member]) extensiontest.Member { return m }}}
		w.run = func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := resourceInput{size: size, count: &w.source}
			if mode == "cancel" {
				source.cancel = cancel
			}
			ok, err := slot.accepts(ctx, source)
			if mode == "cancel" {
				if err == nil || ok {
					t.Fatal("canceled Accepts succeeded")
				}
				return
			}
			if err != nil || ok != (path == "accepts") {
				t.Fatal("Accepts outcome", ok, err)
			}
		}
	case "url":
		address, peerReport := resourcePeer(t, size, mode)
		client := remoteClient(t, &httptest.Server{URL: address}, true)
		w.run = func(t *testing.T) {
			var err error
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel" {
				timer := time.AfterFunc(50*time.Millisecond, cancel)
				defer timer.Stop()
			}
			w.result, err = w.c.AddFromURL(ctx, w.f.manager, owner, RemoteSource{Client: client, URL: address + "/payload.txt"})
			if (mode == "") != (err == nil) {
				t.Fatal("unexpected URL outcome", mode, err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				data, err := os.ReadFile(peerReport)
				if err == nil {
					n, err := strconv.ParseInt(string(data), 10, 64)
					if err != nil || mode == "" && n != size || mode == "error" && n != 0 {
						t.Fatal("remote source count", n, err)
					}
					w.source.Store(n)
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("remote peer omitted byte count")
				}
				time.Sleep(time.Millisecond)
			}
		}
	default:
		result, err := w.c.Add(t.Context(), w.f.manager, owner, input())
		w.result = result
		if path == "fallback" || path == "settle" {
			if err == nil || result.Operation.IsZero() || journal(t, w.f, result.Operation).State != string(Uncertain) {
				t.Fatal("expected uncertain publication", err)
			}
			w.f.backend.setPut(nil)
			if err := w.f.manager.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			w.f.manager, err = New(Dependencies{Store: w.f.Store, Disks: w.f.registry}, config, w.c.Registration())
			if err != nil {
				t.Fatal(err)
			}
			if err := ageAttachmentIntents(t.Context(), w.f); err != nil {
				t.Fatal(err)
			}
			w.run = func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				faultBackend.mode = mode
				if mode == "cancel" {
					w.probe.cancel.Store(&cancel)
				}
				if path == "fallback" {
					_, err = w.f.manager.ReconcilePending(ctx, 10)
				} else {
					_, err = w.f.manager.Settle(ctx, result.Operation, WriterStoppedAndStorageSettled, nil)
				}
				if mode != "" {
					if mode == "cancel" && err == nil {
						t.Error("canceled verification succeeded")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if journal(t, w.f, result.Operation).State != string(Cleaned) {
					t.Error("verified intent not reclaimed")
				}
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			file := attachmentOf(t, result)
			switch path {
			case "full-read":
				w.run = func(t *testing.T) {
					faultBackend.mode = mode
					reader, _, err := w.c.Open(t.Context(), w.f.manager, owner, file.ID())
					if err != nil {
						t.Fatal(err)
					}
					n, err := io.Copy(io.Discard, reader)
					closeErr := reader.Close()
					if mode != "" {
						if err == nil {
							t.Error("incomplete full read verified")
						}
						return
					}
					if err != nil || closeErr != nil || n != size {
						t.Fatal(n, err, closeErr)
					}
				}
			case "small-range":
				w.run = func(t *testing.T) {
					reader, _, err := w.c.OpenSeek(t.Context(), w.f.manager, owner, file.ID())
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Close()
					for _, offset := range []int64{size / 2, size - (32 << 10)} {
						if _, err := reader.Seek(offset, io.SeekStart); err != nil {
							t.Fatal(err)
						}
						if n, err := io.CopyN(io.Discard, reader, 32<<10); err != nil || n != 32<<10 {
							t.Fatal(n, err)
						}
					}
				}
			case "historical-variant":
				changed := Define(extensiontest.Members, "resource", Policy{Disk: testDisk, Cardinality: Single, Accepted: []storage.MediaType{"image/png"}, Variants: []Variant{testThumbnail}})
				if err := w.f.manager.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
				w.f.manager, err = New(Dependencies{Store: w.f.Store, Disks: w.f.registry, Image: w.f.image}, config, changed.Registration())
				if err != nil {
					t.Fatal(err)
				}
				w.c = changed
				w.run = func(t *testing.T) {
					for _, replace := range []bool{false, true} {
						if err := w.f.manager.generate(t.Context(), fileID(result.Operation), replace); !errors.Is(err, storage.LimitExceeded) {
							t.Fatal("historical variant not rejected", err)
						}
					}
				}
			default:
				t.Fatal("unknown resource path", path)
			}
		}
		w.source.Store(0)
		w.written.Store(0)
		w.probe.bytes.Store(0)
		faultBackend.opens.Store(0)
	}
	return w
}

func resourcePeer(t *testing.T, size int64, mode string) (string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestB26ResourcePeer$", "-test.timeout=8m")
	report := filepath.Join(t.TempDir(), "peer-bytes")
	cmd.Env = append(os.Environ(), "FOUNDRY_RESOURCE_PEER_SIZE="+strconv.FormatInt(size, 10), "FOUNDRY_RESOURCE_PEER_REPORT="+report, "FOUNDRY_RESOURCE_PEER_MODE="+mode)
	output, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(output)
	if !scanner.Scan() {
		t.Fatal("remote peer failed to start")
	}
	address := scanner.Text()
	if !strings.HasPrefix(address, "http://127.0.0.1:") {
		t.Fatal("invalid peer address")
	}
	return address, report
}

func TestB26ResourcePeer(t *testing.T) {
	raw := os.Getenv("FOUNDRY_RESOURCE_PEER_SIZE")
	if raw == "" {
		t.Skip("remote generator subprocess only")
	}
	size, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fmt.Println("http://" + listener.Addr().String())
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := os.Getenv("FOUNDRY_RESOURCE_PEER_MODE")
		sendSize := size
		if mode == "limit" {
			sendSize++
		}
		var n int64
		if mode == "error" {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			if mode != "limit" {
				w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))
			}
			w.Header().Set("Content-Type", "text/plain")
			if mode == "cancel" {
				buffer := make([]byte, 64<<10)
				for i := range buffer {
					buffer[i] = 'x'
				}
				for n < sendSize {
					written, err := w.Write(buffer[:min(int64(len(buffer)), sendSize-n)])
					n += int64(written)
					if err != nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			} else {
				n, _ = io.CopyN(w, repeatedByte('x'), sendSize)
			}
		}
		report := os.Getenv("FOUNDRY_RESOURCE_PEER_REPORT")
		_ = os.WriteFile(report+".tmp", []byte(strconv.FormatInt(n, 10)), 0600)
		_ = os.Rename(report+".tmp", report)
	})}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}

// Faults affect bytes returned by a real disk-backed reader, without buffering.
type resourceFaultBackend struct {
	storage.Backend
	mode  string
	opens atomic.Int64
}

func (b *resourceFaultBackend) Open(ctx context.Context, key storage.ObjectKey, options storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	body, info, err := b.Backend.Open(ctx, key, options)
	if err != nil {
		return body, info, err
	}
	b.opens.Add(1)
	return &resourceFaultReader{ReadCloser: body, mode: b.mode, remaining: info.Length - 1}, info, nil
}

type resourceFaultReader struct {
	io.ReadCloser
	mode      string
	remaining int64
	changed   bool
}

func (r *resourceFaultReader) Read(p []byte) (int, error) {
	if r.mode == "short" {
		if r.remaining == 0 {
			return 0, io.EOF
		}
		p = p[:min(int64(len(p)), r.remaining)]
	}
	n, err := r.ReadCloser.Read(p)
	r.remaining -= int64(n)
	if n > 0 && !r.changed && (r.mode == "corrupt" || r.mode == "mismatch") {
		p[0] ^= 1
		r.changed = true
	}
	return n, err
}
