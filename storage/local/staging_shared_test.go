//go:build darwin || linux

package local

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

func sharedStagingConfig(root string) Config {
	c := DefaultConfig(root)
	c.MaxObjectBytes = 8
	c.Staging = StagingLimits{MaxActive: 2, MaxBytes: 8, SmallObjectBytes: 2}
	return c
}

func TestStagingRootBudgetAcrossBackends(t *testing.T) {
	for _, limit := range []string{"bytes", "slots"} {
		t.Run(limit, func(t *testing.T) {
			config := sharedStagingConfig(t.TempDir())
			if limit == "slots" {
				config.Staging.MaxActive = 1
				config.Staging.MaxBytes = 16
			}
			a, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			var once sync.Once
			joined := false
			defer func() {
				once.Do(func() { close(release) })
				if !joined {
					<-done
				}
			}()
			key := stagingKey(t, "first")
			go func() {
				_, err := a.Put(t.Context(), key, io.MultiReader(strings.NewReader("data"), stagingReaderFunc(func([]byte) (int, error) { close(entered); <-release; return 0, io.EOF })), storage.PutOptions{})
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("first writer did not enter")
			}
			b, err := Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			var reads atomic.Int64
			busy, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			_, err = b.Put(busy, stagingKey(t, "second"), stagingReaderFunc(func([]byte) (int, error) { reads.Add(1); return 0, io.EOF }), storage.PutOptions{Size: value.Set[int64](8)})
			ended := busy.Err()
			cancel()
			if ended != nil {
				t.Fatal("capacity rejection waited for the caller deadline", ended)
			}
			if !errors.Is(err, fault.Overloaded) || reads.Load() != 0 {
				t.Fatal("foreign live reservation was ignored", err, reads.Load())
			}
			if _, err := b.Put(t.Context(), stagingKey(t, "small"), strings.NewReader("ok"), storage.PutOptions{Size: value.Set[int64](2)}); err != nil {
				t.Fatal("small write lost its capacity", err)
			}
			once.Do(func() { close(release) })
			err = <-done
			joined = true
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Put(t.Context(), stagingKey(t, "after"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
				t.Fatal("publication retained the foreign reservation", err)
			}
		})
	}
}

func TestStagingRootBudgetSimultaneousAdmission(t *testing.T) {
	config := sharedStagingConfig(t.TempDir())
	var peers [2]*Backend
	for i := range peers {
		var err error
		peers[i], err = Open(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		defer peers[i].Close()
	}
	start := make(chan struct{})
	entered := make(chan int, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var results [2]chan error
	var cancels [2]context.CancelFunc
	var joined [2]bool
	for i, peer := range peers {
		ctx, cancel := context.WithCancel(t.Context())
		cancels[i] = cancel
		results[i] = make(chan error, 1)
		key := stagingKey(t, []string{"race-a", "race-b"}[i])
		go func() {
			<-start
			_, err := peer.Put(ctx, key, stagingReaderFunc(func([]byte) (int, error) { entered <- i; <-release; return 0, io.EOF }), storage.PutOptions{})
			results[i] <- err
		}()
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		for i := range peers {
			cancels[i]()
			if !joined[i] {
				<-results[i]
			}
		}
	}()
	close(start)
	var winner int
	select {
	case winner = <-entered:
	case <-time.After(time.Second):
		t.Fatal("serialized admission did not select a winner promptly")
	}
	loser := 1 - winner
	var err error
	select {
	case err = <-results[loser]:
		joined[loser] = true
	case <-entered:
		t.Fatal("both contenders consumed input under one root budget")
	case <-time.After(time.Second):
		t.Fatal("excess contender waited for capacity")
	}
	if !errors.Is(err, fault.Overloaded) {
		t.Fatal("unexpected rejection", err)
	}
	releaseOnce.Do(func() { close(release) })
	err = <-results[winner]
	joined[winner] = true
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peers[loser].Put(t.Context(), stagingKey(t, "race-recovered"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
		t.Fatal("completed reservation retained capacity", err)
	}
}

func TestStagingRootBudgetSurvivesProcessCrash(t *testing.T) {
	const childRoot = "FOUNDRY_TEST_STAGING_CRASH_ROOT"
	if root := os.Getenv(childRoot); root != "" {
		backend, err := Open(t.Context(), sharedStagingConfig(root))
		if err != nil {
			t.Fatal(err)
		}
		_, err = backend.Put(t.Context(), stagingKey(t, "crashed"), io.MultiReader(strings.NewReader("data"), stagingReaderFunc(func([]byte) (int, error) { os.Exit(73); return 0, io.EOF })), storage.PutOptions{})
		t.Fatal("crash callback did not exit", err)
	}
	config := sharedStagingConfig(t.TempDir())
	a, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestStagingRootBudgetSurvivesProcessCrash$")
	child.Env = append(os.Environ(), childRoot+"="+config.Root)
	output, err := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatal("child did not crash as intended", err, string(output))
	}
	b, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	busy, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	_, err = b.Put(busy, stagingKey(t, "blocked"), strings.NewReader("next"), storage.PutOptions{})
	ended := busy.Err()
	cancel()
	if ended != nil {
		t.Fatal("capacity rejection waited for the caller deadline", ended)
	}
	if !errors.Is(err, fault.Overloaded) {
		t.Fatal("crashed staging disappeared from the root budget", err)
	}
	if n, err := a.Prune(t.Context(), time.Now(), MaxPrune); err != nil || n != 1 {
		t.Fatal("unlocked crash record was not reclaimed", n, err)
	}
	if _, err := b.Put(t.Context(), stagingKey(t, "recovered"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
		t.Fatal("foreign prune did not release the root reservation", err)
	}
}

func TestStagingRootBudgetCountsTwoLiveWriters(t *testing.T) {
	config := sharedStagingConfig(t.TempDir())
	config.Staging.MaxActive, config.Staging.MaxBytes = 3, 16
	config.MaxScan = 1
	var peers [2]*Backend
	var release [2]chan struct{}
	var done [2]chan error
	var once [2]sync.Once
	var joined [2]bool
	entered := make(chan struct{}, 2)
	for i := range peers {
		var err error
		peers[i], err = Open(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		defer peers[i].Close()
	}
	defer func() {
		for i := range peers {
			once[i].Do(func() { close(release[i]) })
			if !joined[i] {
				<-done[i]
			}
		}
	}()
	for i := range peers {
		release[i], done[i] = make(chan struct{}), make(chan error, 1)
		key := stagingKey(t, []string{"live-a", "live-b"}[i])
		go func() {
			_, err := peers[i].Put(t.Context(), key, io.MultiReader(strings.NewReader("data"), stagingReaderFunc(func([]byte) (int, error) { entered <- struct{}{}; <-release[i]; return 0, io.EOF })), storage.PutOptions{})
			done[i] <- err
		}()
	}
	for range peers {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("writer did not enter")
		}
	}

	page, err := peers[0].List(t.Context(), storage.ListOptions{Limit: 10})
	if err != nil || len(page.Objects) != 0 {
		t.Fatal("staging consumed the published-object scan ceiling", err)
	}

	busy, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	_, err = peers[0].Put(busy, stagingKey(t, "third"), strings.NewReader("next"), storage.PutOptions{})
	ended := busy.Err()
	cancel()
	if ended != nil {
		t.Fatal("capacity rejection waited for the caller deadline", ended)
	}
	if !errors.Is(err, fault.Overloaded) {
		t.Fatal("combined live reservations exceeded the root budget", err)
	}
	if n, err := peers[0].Prune(t.Context(), time.Now(), MaxPrune); err != nil || n != 0 {
		t.Fatal("live reservations pruned", n, err)
	}
	once[0].Do(func() { close(release[0]) })
	err = <-done[0]
	joined[0] = true
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peers[0].Put(t.Context(), stagingKey(t, "replacement"), strings.NewReader("next"), storage.PutOptions{}); err != nil {
		t.Fatal("completed writer did not return its reservation", err)
	}
	once[1].Do(func() { close(release[1]) })
	err = <-done[1]
	joined[1] = true
	if err != nil {
		t.Fatal(err)
	}
}
