//go:build darwin || linux

package local

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestStagingBurstAcrossProcessesAdmitsExactCapacity(t *testing.T) {
	const rootEnv = "FOUNDRY_TEST_STAGING_BURST_ROOT"
	const idEnv = "FOUNDRY_TEST_STAGING_BURST_ID"
	const contenders = 8
	const capacity = 3
	configured := func(root string) Config {
		c := sharedStagingConfig(root)
		c.MaxScan = 1
		c.Staging.MaxActive = capacity
		c.Staging.MaxBytes = capacity * c.MaxObjectBytes
		return c
	}
	if root := os.Getenv(rootEnv); root != "" {
		backend, err := Open(t.Context(), configured(root))
		if err != nil {
			t.Fatal(err)
		}
		defer backend.Close()
		input := bufio.NewReader(os.Stdin)
		fmt.Fprintln(os.Stdout, "ready")
		if line, err := input.ReadString('\n'); err != nil || line != "start\n" {
			t.Fatal("missing start barrier", err)
		}
		release := make(chan struct{})
		var workers sync.WaitGroup
		for i := range contenders {
			key := stagingKey(t, fmt.Sprintf("burst-%s-%d", os.Getenv(idEnv), i))
			workers.Add(1)
			go func() {
				defer workers.Done()
				source := io.MultiReader(strings.NewReader("data"), stagingReaderFunc(func([]byte) (int, error) { fmt.Fprintln(os.Stdout, "admitted"); <-release; return 0, io.EOF }))
				_, err := backend.Put(t.Context(), key, source, storage.PutOptions{})
				if errors.Is(err, fault.Overloaded) {
					fmt.Fprintln(os.Stdout, "rejected")
				} else if err != nil {
					t.Error(err)
					fmt.Fprintln(os.Stdout, "failed")
				}
			}()
		}
		line, err := input.ReadString('\n')
		close(release)
		workers.Wait()
		if err != nil || line != "release\n" {
			t.Fatal("missing release barrier", err)
		}
		return
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	type report struct {
		process int
		line    string
	}
	reports := make(chan report, 64)
	var commands [2]*exec.Cmd
	var inputs [2]io.WriteCloser
	var diagnostics [2]bytes.Buffer
	for i := range commands {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStagingBurstAcrossProcessesAdmitsExactCapacity$")
		cmd.Env = append(os.Environ(), rootEnv+"="+root, idEnv+"="+fmt.Sprint(i))
		cmd.Stderr = &diagnostics[i]
		var err error
		inputs[i], err = cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands[i] = cmd
		t.Cleanup(func() {
			_ = inputs[i].Close()
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		go func() {
			scanner := bufio.NewScanner(output)
			for scanner.Scan() {
				reports <- report{i, scanner.Text()}
			}
			if scanner.Err() != nil {
				reports <- report{i, "scanner failed"}
			}
		}()
	}
	for range commands {
		select {
		case r := <-reports:
			if r.line != "ready" {
				t.Fatal("unexpected child startup", r)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("child did not start")
		}
	}
	for _, input := range inputs {
		if _, err := io.WriteString(input, "start\n"); err != nil {
			t.Fatal(err)
		}
	}
	admitted, rejected := 0, 0
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for range len(commands) * contenders {
		select {
		case r := <-reports:
			switch r.line {
			case "admitted":
				admitted++
			case "rejected":
				rejected++
			default:
				t.Fatal("unexpected admission result", r)
			}
		case <-deadline.C:
			t.Fatal("contenders waited for capacity", admitted, rejected)
		}
	}
	if admitted != capacity || rejected != len(commands)*contenders-capacity {
		t.Fatal("burst did not fill exactly the root capacity", admitted, rejected)
	}
	entries, err := os.ReadDir(filepath.Join(root, stagingDirectory))
	if err != nil {
		t.Fatal(err)
	}
	reservations := 0
	for _, entry := range entries {
		if stagingName(entry.Name()) {
			reservations++
		}
	}
	if reservations != capacity {
		t.Fatal("rejected contenders left reservations", reservations)
	}
	for _, input := range inputs {
		if _, err := io.WriteString(input, "release\n"); err != nil {
			t.Fatal(err)
		}
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal("child failed", err, diagnostics[i].String())
		}
	}
	entries, err = os.ReadDir(filepath.Join(root, stagingDirectory))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if stagingName(entry.Name()) {
			t.Fatal("publication retained a reservation", entry.Name())
		}
	}
}
