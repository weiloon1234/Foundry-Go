package s3_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Route a synthetic R2 request to the local TLS peer. No live provider is reached
// and this does not certify R2's actual service-side precondition evaluation.
type localPeerClient struct {
	client interface {
		Do(*http.Request) (*http.Response, error)
	}
	endpoint *url.URL
}

func (c localPeerClient) Do(request *http.Request) (*http.Response, error) {
	next := request.Clone(request.Context())
	address := *request.URL
	address.Scheme = c.endpoint.Scheme
	address.Host = c.endpoint.Host
	next.URL = &address
	next.Host = ""
	return c.client.Do(next)
}
func r2WireDisk(t *testing.T, peer http.Handler) *storage.Disk {
	t.Helper()
	disk, _ := r2WireBackend(t, peer, nil)
	return disk
}
func r2WireBackend(t *testing.T, peer http.Handler, configure func(*s3.Config)) (*storage.Disk, *s3.Backend) {
	t.Helper()
	return wireDisk(t, peer, func(c *s3.Config) {
		endpoint, err := url.Parse(c.Endpoint)
		if err != nil {
			t.Fatal(err)
		}
		c.HTTPClient = localPeerClient{client: c.HTTPClient, endpoint: endpoint}
		c.Provider = s3.R2
		c.Region = "auto"
		c.Endpoint = "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com"
		if configure != nil {
			configure(c)
		}
	})
}
func TestR2ConditionalWritesRequireBoundedSingleRequest(t *testing.T) {
	peer := &uploadPeer{}
	disk := r2WireDisk(t, peer)
	caps := disk.Capabilities()
	if !caps.ConditionalCreate || !caps.ConditionalReplace || caps.ConditionalWriteMaxBytes != s3.MinPartBytes {
		t.Fatal("R2 conditional bound missing")
	}
	if _, err := disk.PutBytes(t.Context(), objectKey(t), []byte("small"), storage.PutOptions{Condition: storage.IfAbsent()}); err != nil {
		t.Fatal(err)
	}
	peer.mu.Lock()
	if peer.puts != 1 || peer.creates != 0 || peer.metadata.Get("If-None-Match") != "*" {
		t.Error("conditional create was not encoded on PutObject")
	}
	peer.mu.Unlock()
	// A declared oversupply is rejected before upload. The reader records whether
	// any bytes were requested, independently of provider traffic.
	source := &zeroSource{remaining: s3.MinPartBytes + 1}
	if _, err := disk.Put(t.Context(), objectKey(t), source, storage.PutOptions{Size: value.Set(s3.MinPartBytes + 1), Condition: storage.IfAbsent()}); !errors.Is(err, storage.Unsupported) || source.read != 0 {
		t.Fatal("conditional multipart consumed source", err)
	}
	condition, err := storage.IfMatch(`"published"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.PutBytes(t.Context(), objectKey(t), []byte("replace"), storage.PutOptions{Condition: condition}); err != nil {
		t.Fatal(err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.puts != 2 || peer.creates != 0 || peer.metadata.Get("If-Match") != `"published"` {
		t.Fatal("conditional replacement lost its header")
	}
}

func TestR2ConditionalTransferHelpersInferKnownSize(t *testing.T) {
	peer := &uploadPeer{}
	destination := r2WireDisk(t, peer)
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	condition := storage.PutOptions{Condition: storage.IfAbsent()}
	if _, err := destination.PutFile(t.Context(), objectKey(t), path, condition); err != nil {
		t.Fatal("regular file size was not inferred before capability validation", err)
	}
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	source, err := storage.NewDisk("copy-source", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close(context.Background())
	if _, err := source.PutBytes(t.Context(), objectKey(t), []byte("payload"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.CopyTo(t.Context(), objectKey(t), destination, objectKey(t), storage.CopyOptions{Destination: condition}); err != nil {
		t.Fatal("source object size was not inferred before capability validation", err)
	}
	if source.Stats().Active != 0 {
		t.Fatal("copy retained its source reader")
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.puts != 2 || peer.creates != 0 || peer.published != 7 || peer.metadata.Get("If-None-Match") != "*" {
		t.Fatal("conditional helpers lost single-request publication or preconditions")
	}
}
