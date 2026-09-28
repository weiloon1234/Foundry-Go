package s3_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func TestR2RejectsUnicodeAliasesBeforeIO(t *testing.T) {
	var calls atomic.Int32
	var config s3.Config
	disk, backend := r2WireBackend(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }), func(c *s3.Config) {
		c.PublicBase, _ = storage.ParsePublicBase("https://cdn.example.test")
		config = *c
	})
	key, _ := storage.ParseKey("unicode/e\u0301")
	prefix, _ := storage.ParsePrefix("unicode/e\u0301/")
	source := &zeroSource{remaining: 1}
	for name, call := range map[string]func() error{
		"put": func() error { _, err := disk.Put(t.Context(), key, source, storage.PutOptions{}); return err },
		"open": func() error {
			body, _, err := disk.Open(t.Context(), key, storage.ReadOptions{})
			if body != nil {
				body.Close()
				t.Error("alias returned a reader")
			}
			return err
		},
		"stat":   func() error { _, err := disk.Stat(t.Context(), key, storage.ReadOptions{}); return err },
		"delete": func() error { return disk.Delete(t.Context(), key, storage.DeleteOptions{}) },
		"list": func() error {
			_, err := disk.List(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 1})
			return err
		},
		"sign": func() error {
			_, err := disk.TemporaryURL(t.Context(), key, storage.LinkOptions{ExpiresIn: time.Minute})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, storage.Unsupported) {
				t.Fatal("alias accepted", err)
			}
		})
	}
	if source.read != 0 || calls.Load() != 0 || !disk.Capabilities().RequiresNFCKeys {
		t.Fatal("alias reached provider/source or capability missing")
	}
	// Exercise adapter maintenance and construction boundaries as well.
	if _, err := backend.ListUploads(t.Context(), storage.ListOptions{Prefix: prefix, Limit: 1}); !errors.Is(err, storage.Unsupported) {
		t.Fatal("maintenance prefix alias accepted", err)
	}
	if _, err := backend.Locate(key); !errors.Is(err, storage.Unsupported) {
		t.Fatal("locator alias accepted", err)
	}
	if _, err := backend.PublicURL(t.Context(), key); !errors.Is(err, storage.Unsupported) {
		t.Fatal("public URL alias accepted", err)
	}
	config.Namespace = prefix
	if _, err := s3.Prepare(config); !errors.Is(err, storage.Unsupported) {
		t.Fatal("namespace alias accepted", err)
	}
	if calls.Load() != 0 {
		t.Fatal("alias made a maintenance request")
	}
}
