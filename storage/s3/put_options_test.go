package s3_test

import (
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func TestPutSendsTypedObjectMetadata(t *testing.T) {
	for _, size := range []int64{3, s3.MinPartBytes + 17} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			peer := &uploadPeer{}
			disk := uploadDisk(t, peer)
			metadata := storage.ObjectMetadata{CacheControl: "public, max-age=60", ContentDisposition: `attachment; filename="a.bin"`, ContentEncoding: "identity", StorageClass: "STANDARD_IA", EncryptionKey: "alias/fixture", Custom: map[string]string{"tenant": "t-1"}}
			if _, err := disk.Put(t.Context(), objectKey(t), &zeroSource{remaining: size}, storage.PutOptions{ContentType: "application/octet-stream", Metadata: metadata}); err != nil {
				t.Fatal(err)
			}
			peer.mu.Lock()
			defer peer.mu.Unlock()
			h := peer.metadata
			if h.Get("Cache-Control") != "public, max-age=60" || h.Get("Content-Disposition") != `attachment; filename="a.bin"` || h.Get("Content-Encoding") != "identity" || h.Get("X-Amz-Storage-Class") != "STANDARD_IA" || h.Get("X-Amz-Server-Side-Encryption") != "aws:kms" || h.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id") != "alias/fixture" || h.Get("X-Amz-Meta-Tenant") != "t-1" {
				t.Fatal("object metadata was not sent with the publication", h)
			}
			if h.Get("X-Amz-Meta-Foundry-Sha256") == "" && size <= s3.MinPartBytes {
				t.Fatal("framework checksum metadata was displaced")
			}
		})
	}
	for _, invalid := range []storage.ObjectMetadata{{CacheControl: "bad\n"}, {Custom: map[string]string{"foundry-sha256": "x"}}, {Custom: map[string]string{"Upper": "x"}}, {StorageClass: "lower"}} {
		peer := &uploadPeer{}
		disk := uploadDisk(t, peer)
		_, err := disk.PutBytes(t.Context(), objectKey(t), []byte("x"), storage.PutOptions{Metadata: invalid})
		peer.mu.Lock()
		puts := peer.puts
		peer.mu.Unlock()
		if !errors.Is(err, storage.Invalid) || puts != 0 {
			t.Fatal("invalid metadata reached the provider", err)
		}
	}
}

func TestConcurrentMultipartPartsPreserveOrderAndBoundInFlight(t *testing.T) {
	peer := &uploadPeer{}
	var inflight, peak atomic.Int32
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && r.URL.Query().Has("uploadId") {
			current := inflight.Add(1)
			for {
				seen := peak.Load()
				if current <= seen || peak.CompareAndSwap(seen, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			defer inflight.Add(-1)
		}
		peer.ServeHTTP(w, r)
	}), func(c *s3.Config) { c.PartConcurrency = 2 })
	size := 3*s3.MinPartBytes + 17
	source := &zeroSource{remaining: size}
	stored, err := disk.Put(t.Context(), objectKey(t), source, storage.PutOptions{})
	if err != nil || stored.Object.Size != size {
		t.Fatal(err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.parts != 4 || peer.completes != 1 || peer.published != size || peer.invalidMD5 || source.read != size {
		t.Fatal("concurrent parts changed the published object", peer.parts, peer.published)
	}
	for number, expected := range map[string]int64{"1": s3.MinPartBytes, "2": s3.MinPartBytes, "3": s3.MinPartBytes, "4": 17} {
		if peer.partSizes[number] != expected {
			t.Fatal("part numbering changed", number, peer.partSizes[number])
		}
	}
	if peak.Load() > 2 {
		t.Fatal("part concurrency exceeded its bound", peak.Load())
	}
}

func TestPublicationWithoutVerificationReadIsApplied(t *testing.T) {
	peer := &uploadPeer{}
	disk, _ := wireDisk(t, peer, func(c *s3.Config) { c.VerifyPublication = false })
	stored, err := disk.PutBytes(t.Context(), objectKey(t), []byte("abc"), storage.PutOptions{ContentType: "text/plain"})
	if err != nil || stored.Object.ETag != `"published"` || stored.Object.Modified.IsZero() || stored.Object.Size != 3 || !stored.Object.Checksum.IsSet() {
		t.Fatal("acknowledged publication was not returned", stored, err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.heads != 0 {
		t.Fatal("disabled verification still read the object")
	}
}
