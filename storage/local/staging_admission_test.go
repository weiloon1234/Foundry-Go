//go:build darwin || linux

package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

type stagingAdmissionObserver struct {
	storage.Backend
	key     storage.ObjectKey
	entered chan struct{}
}

func (b *stagingAdmissionObserver) Put(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	if key == b.key {
		close(b.entered)
	}
	return b.Backend.Put(ctx, key, source, options)
}

func TestStagingExhaustionPreservesSmallDiskOperation(t *testing.T) {
	config := sharedStagingConfig(t.TempDir())
	config.Staging.MaxActive = 1
	backend, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	excess := stagingKey(t, "excess")
	observed := &stagingAdmissionObserver{Backend: backend, key: excess, entered: make(chan struct{})}
	diskConfig := storage.DefaultConfig()
	diskConfig.MaxActive = 2
	diskConfig.MaxObjectBytes = 8
	disk, err := storage.NewDisk("staging", observed, diskConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close(context.Background())
	entered, release, first := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	joined := false
	defer func() {
		once.Do(func() { close(release) })
		if !joined {
			<-first
		}
	}()
	held := stagingKey(t, "held")
	go func() {
		_, err := disk.Put(t.Context(), held, io.MultiReader(strings.NewReader("data"), stagingReaderFunc(func([]byte) (int, error) { close(entered); <-release; return 0, io.EOF })), storage.PutOptions{})
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("large writer did not enter")
	}
	ctx, cancel := context.WithCancel(t.Context())
	second := make(chan error, 1)
	secondJoined := false
	defer func() {
		cancel()
		if !secondJoined {
			<-second
		}
	}()
	go func() {
		_, err := disk.Put(ctx, excess, strings.NewReader("next"), storage.PutOptions{})
		second <- err
	}()
	// This callback runs after Disk admitted the second operation. A capacity
	// waiter here would consume the last disk slot and starve the small write.
	select {
	case <-observed.entered:
	case <-time.After(time.Second):
		t.Fatal("second disk slot was not admitted")
	}
	small, cancelSmall := context.WithTimeout(t.Context(), 500*time.Millisecond)
	_, err = disk.Put(small, stagingKey(t, "small"), strings.NewReader("ok"), storage.PutOptions{Size: value.Set[int64](2)})
	cancelSmall()
	if err != nil {
		t.Fatal("large capacity wait consumed the small operation slot", err)
	}
	select {
	case err = <-second:
		secondJoined = true
	case <-time.After(time.Second):
		t.Fatal("exhausted staging did not reject promptly")
	}
	if !errors.Is(err, fault.Overloaded) {
		t.Fatal("missing typed capacity error", err)
	}
	once.Do(func() { close(release) })
	err = <-first
	joined = true
	if err != nil {
		t.Fatal(err)
	}
}

func TestStagingSolitaryWriteIgnoresPublishedNamespace(t *testing.T) {
	config := sharedStagingConfig(t.TempDir())
	config.MaxScan = 1
	config.Sync = false
	backend, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	target := stagingKey(t, "solitary-large")
	targetDirectory, _ := address(target)
	protected := ""
	for i := range 512 {
		key := stagingKey(t, fmt.Sprintf("published-%d", i))
		if _, err := backend.Put(t.Context(), key, strings.NewReader("ok"), storage.PutOptions{Size: value.Set[int64](2)}); err != nil {
			t.Fatal(err)
		}
		directory, _ := address(key)
		if directory != targetDirectory {
			protected = filepath.Join(config.Root, directory)
		}
	}
	// A published shard that cannot be enumerated makes any accidental full-root
	// admission scan fail deterministically, without a timing/performance assertion.
	if os.Geteuid() != 0 {
		if err := os.Chmod(protected, 0100); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(protected, 0700)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, err := backend.Put(ctx, target, strings.NewReader("next"), storage.PutOptions{}); err != nil {
		t.Fatal("solitary admission traversed published objects", err)
	}
	reader, _, err := backend.Open(t.Context(), target, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || string(data) != "next" {
		t.Fatal("cross-directory publication changed bytes", err, closeErr)
	}
}
