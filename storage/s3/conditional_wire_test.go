package s3

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type conditionalObject struct {
	path, tag string
	size      int64
	metadata  http.Header
}
type conditionalUpload struct {
	key      string
	metadata http.Header
	parts    map[int]conditionalObject
}

// This SDK peer persists bytes to test-owned files. It does not certify service
// semantics; only the env-gated test against an actual R2 namespace can do that.
type conditionalPeer struct {
	mu        sync.Mutex
	directory string
	next      int
	objects   map[string]conditionalObject
	uploads   map[string]*conditionalUpload
	invalid   bool
	partBytes int64
}

func (p *conditionalPeer) receive(r *http.Request) (conditionalObject, error) {
	f, err := os.CreateTemp(p.directory, "bytes-")
	if err != nil {
		return conditionalObject{}, err
	}
	md := md5.New()
	sha := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, md, sha), r.Body)
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if r.Header.Get("Content-MD5") != base64.StdEncoding.EncodeToString(md.Sum(nil)) {
		p.invalid = true
	}
	return conditionalObject{path: f.Name(), size: n, tag: `"` + hex.EncodeToString(sha.Sum(nil)) + `"`, metadata: r.Header.Clone()}, err
}
func (p *conditionalPeer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reject := func(status int, code string) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", code)
	}
	key := strings.TrimPrefix(r.URL.Path, "/fixture-bucket/")
	q := r.URL.Query()
	id := q.Get("uploadId")
	condition := func() bool {
		old, exists := p.objects[key]
		if r.Header.Get("If-None-Match") == "*" && exists {
			return false
		}
		return r.Header.Get("If-Match") == "" || exists && r.Header.Get("If-Match") == old.tag
	}
	switch {
	case r.Method == "POST" && q.Has("uploads"):
		p.next++
		id = strconv.Itoa(p.next)
		if r.Header.Get("X-Amz-Meta-"+checksumMetadata) == "" {
			p.invalid = true
		}
		p.uploads[id] = &conditionalUpload{key: key, metadata: r.Header.Clone(), parts: map[int]conditionalObject{}}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, id)
	case r.Method == "PUT" && id != "":
		up := p.uploads[id]
		if up == nil {
			reject(404, "NoSuchUpload")
			return
		}
		part, err := p.receive(r)
		if err != nil {
			reject(400, "IncompleteBody")
			return
		}
		if part.size > p.partBytes {
			p.invalid = true
		}
		number, _ := strconv.Atoi(q.Get("partNumber"))
		up.parts[number] = part
		w.Header().Set("ETag", part.tag)
	case r.Method == "POST" && id != "":
		up := p.uploads[id]
		if up == nil {
			reject(404, "NoSuchUpload")
			return
		}
		var manifest struct {
			Parts []struct {
				Number int    `xml:"PartNumber"`
				Tag    string `xml:"ETag"`
			} `xml:"Part"`
		}
		if xml.NewDecoder(r.Body).Decode(&manifest) != nil || len(manifest.Parts) != len(up.parts) {
			reject(400, "InvalidPart")
			return
		}
		if !condition() {
			reject(412, "PreconditionFailed")
			return
		}
		file, err := os.CreateTemp(p.directory, "object-")
		if err != nil {
			reject(500, "InternalError")
			return
		}
		hash := sha256.New()
		var size int64
		for i, entry := range manifest.Parts {
			part := up.parts[entry.Number]
			if entry.Number != i+1 || entry.Tag != part.tag || i < len(manifest.Parts)-1 && part.size != p.partBytes {
				p.invalid = true
			}
			source, err := os.Open(part.path)
			if err != nil {
				p.invalid = true
				continue
			}
			n, copyErr := io.Copy(io.MultiWriter(file, hash), source)
			_ = source.Close()
			if copyErr != nil {
				p.invalid = true
			}
			size += n
		}
		_ = file.Close()
		tag := `"` + hex.EncodeToString(hash.Sum(nil)) + `"`
		if up.metadata.Get("X-Amz-Meta-"+checksumMetadata) != hex.EncodeToString(hash.Sum(nil)) {
			p.invalid = true
		}
		p.objects[key] = conditionalObject{path: file.Name(), size: size, tag: tag, metadata: up.metadata}
		for _, part := range up.parts {
			_ = os.Remove(part.path)
		}
		delete(p.uploads, id)
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<CompleteMultipartUploadResult><ETag>%s</ETag></CompleteMultipartUploadResult>`, tag)
	case r.Method == "DELETE" && id != "":
		if up := p.uploads[id]; up != nil {
			for _, part := range up.parts {
				_ = os.Remove(part.path)
			}
			delete(p.uploads, id)
		}
		w.WriteHeader(204)
	case r.Method == "GET" && id != "":
		if p.uploads[id] == nil {
			reject(404, "NoSuchUpload")
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<ListPartsResult><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber></Part></ListPartsResult>`)
	case r.Method == "GET" && q.Has("uploads"):
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<ListMultipartUploadsResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated>`)
		ids := make([]string, 0, len(p.uploads))
		for id := range p.uploads {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(w, `<Upload><Key>%s</Key><UploadId>%s</UploadId><Initiated>2026-11-13T00:00:00Z</Initiated></Upload>`, url.QueryEscape(p.uploads[id].key), id)
		}
		io.WriteString(w, `</ListMultipartUploadsResult>`)
	case r.Method == "PUT":
		object, err := p.receive(r)
		if err != nil {
			reject(400, "IncompleteBody")
			return
		}
		if !condition() {
			_ = os.Remove(object.path)
			reject(412, "PreconditionFailed")
			return
		}
		p.objects[key] = object
		w.Header().Set("ETag", object.tag)
	case r.Method == "GET" && q.Get("list-type") == "2":
		keys := make([]string, 0, len(p.objects))
		for key := range p.objects {
			if strings.HasPrefix(key, q.Get("prefix")) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated>`)
		for _, key := range keys {
			object := p.objects[key]
			fmt.Fprintf(w, `<Contents><Key>%s</Key><ETag>%s</ETag><Size>%d</Size><LastModified>2026-11-13T00:00:00Z</LastModified></Contents>`, url.QueryEscape(key), object.tag, object.size)
		}
		io.WriteString(w, `</ListBucketResult>`)
	case r.Method == "DELETE":
		if !condition() {
			reject(412, "PreconditionFailed")
			return
		}
		if object, ok := p.objects[key]; ok {
			_ = os.Remove(object.path)
			delete(p.objects, key)
		}
		w.WriteHeader(204)
	case r.Method == "GET" || r.Method == "HEAD":
		object, ok := p.objects[key]
		if !ok {
			reject(404, "NoSuchKey")
			return
		}
		if !condition() {
			reject(412, "PreconditionFailed")
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(object.size, 10))
		w.Header().Set("Content-Type", object.metadata.Get("Content-Type"))
		w.Header().Set("ETag", object.tag)
		w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		for k, values := range object.metadata {
			if strings.HasPrefix(strings.ToLower(k), "x-amz-meta-") {
				w.Header()[k] = values
			}
		}
		if r.Method == "GET" {
			f, err := os.Open(object.path)
			if err != nil {
				p.invalid = true
				return
			}
			_, _ = io.Copy(w, f)
			_ = f.Close()
		}
	default:
		reject(400, "UnexpectedFixtureOperation")
	}
}

type conditionalRedirect struct {
	client   *http.Client
	endpoint *url.URL
}

func (c conditionalRedirect) Do(r *http.Request) (*http.Response, error) {
	next := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = c.endpoint.Scheme, c.endpoint.Host
	next.URL = &u
	next.Host = ""
	return c.client.Do(next)
}
func conditionalWire(t *testing.T) (*storage.Disk, *Backend, *conditionalGate, *conditionalPeer) {
	return conditionalWireProfile(t, R2)
}
func conditionalWireProfile(t *testing.T, provider Provider) (*storage.Disk, *Backend, *conditionalGate, *conditionalPeer) {
	t.Helper()
	peer := &conditionalPeer{directory: t.TempDir(), objects: map[string]conditionalObject{}, uploads: map[string]*conditionalUpload{}, partBytes: 8 << 20}
	server := httptest.NewTLSServer(peer)
	t.Cleanup(server.Close)
	endpoint, _ := url.Parse(server.URL)
	gate := &conditionalGate{client: conditionalRedirect{server.Client(), endpoint}}
	cfg := R2Config("fixture-bucket", "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com", credentials.NewStaticCredentialsProvider("fixture", "fixture", ""))
	cfg.HTTPClient = gate
	cfg.PartAttempts = 1
	cfg.Provider = provider
	if provider == Compatible {
		cfg.Compatible = CompatibleCapabilities{ConditionalCreate: true, ConditionalReplace: true, ConditionalDelete: true, RequiresNFCKeys: true}
	}
	b, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if provider == R2 {
		return candidateDisk(t, b), b, gate, peer
	}
	disk, err := storage.NewDisk("conditional-wire", b, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disk.Close(context.Background()) })
	return disk, b, gate, peer
}
func TestR2ConditionalPublicationCandidateWireMatrix(t *testing.T) {
	for _, size := range []int64{8 << 20, (8 << 20) + 1} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			disk, b, gate, peer := conditionalWire(t)
			runConditionalMatrix(t, disk, b, gate, size)
			peer.mu.Lock()
			defer peer.mu.Unlock()
			if peer.invalid {
				t.Fatal("wire metadata, checksum, part size or manifest invalid")
			}
		})
	}
}
func TestR2ShippingProfileStillRejectsUncertifiedCreateAndReplace(t *testing.T) {
	_, b, _, _ := conditionalWire(t)
	key, _ := storage.ParseKey("restricted")
	for _, condition := range []storage.WriteCondition{storage.IfAbsent(), conditionFor(t, `"old"`)} {
		source := &conditionalCountReader{}
		_, err := b.Put(t.Context(), key, source, conditionalOptions(b.config.PartBytes+1, 'a', condition))
		if !errors.Is(err, storage.Unsupported) || source.read != 0 {
			t.Fatal("uncertified multipart condition reached source", err)
		}
	}
}

type conditionalCountReader struct{ read int }

func (r *conditionalCountReader) Read(p []byte) (int, error) { r.read++; return 0, io.EOF }

func TestLargeConditionalMultipartStreamsDefaultParts(t *testing.T) {
	disk, b, _, peer := conditionalWire(t)
	if b.config.PartBytes != 8<<20 || b.config.PartConcurrency != 1 {
		t.Fatal("default multipart resource budget changed")
	}
	const size int64 = 65 << 20
	key, _ := storage.ParseKey("large")
	options := conditionalOptions(size, 'x', storage.IfAbsent())
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	first, err := disk.Put(t.Context(), key, io.LimitReader(conditionalByte('x'), size), options)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal("large conditional multipart publication failed", err)
	}
	if after.TotalAlloc-before.TotalAlloc > 32<<20 {
		t.Fatal("multipart publication allocated content-sized memory")
	}
	conditionalRead(t, disk, key, size, 'x')
	if _, err := conditionalPut(t.Context(), disk, key, size, 'y', conditionFor(t, first.Object.ETag)); err != nil {
		t.Fatal(err)
	}
	conditionalRead(t, disk, key, size, 'y')
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.invalid || len(peer.uploads) != 0 {
		t.Fatal("invalid default-part transfer or retained staging")
	}
}
