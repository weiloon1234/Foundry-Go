package local_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

func TestLocalProcessConditionalCreate(t *testing.T) {
	if root := os.Getenv("FOUNDRY_STORAGE_CHILD_ROOT"); root != "" {
		backend, err := local.Open(t.Context(), local.DefaultConfig(root))
		if err != nil {
			t.Fatal(err)
		}
		defer backend.Close()
		disk, err := storage.NewDisk("child", backend, storage.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		defer disk.Close(context.Background())
		_, err = disk.PutBytes(t.Context(), key(t, "shared/conditional"), []byte("published"), storage.PutOptions{Condition: storage.IfAbsent()})
		switch {
		case err == nil:
			fmt.Println("foundry-child-created")
		case errors.Is(err, storage.PreconditionFailed):
			fmt.Println("foundry-child-conflict")
		default:
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	disk, _ := openLocal(t, root)
	var outputs [2]bytes.Buffer
	var commands [2]*exec.Cmd
	for i := range commands {
		commands[i] = exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLocalProcessConditionalCreate$")
		commands[i].Env = append(os.Environ(), "FOUNDRY_STORAGE_CHILD_ROOT="+root)
		commands[i].Stdout = &outputs[i]
		commands[i].Stderr = &outputs[i]
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if commands[i].ProcessState == nil {
				_ = commands[i].Process.Kill()
				_ = commands[i].Wait()
			}
		})
	}
	winners, conflicts := 0, 0
	for i, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatal("child writer failed", err, outputs[i].String())
		}
		text := outputs[i].String()
		if strings.Contains(text, "foundry-child-created") {
			winners++
		}
		if strings.Contains(text, "foundry-child-conflict") {
			conflicts++
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatal("processes did not share publication exclusion")
	}
	data, _, err := disk.ReadBytes(t.Context(), key(t, "shared/conditional"), 32, storage.ReadOptions{})
	if err != nil || string(data) != "published" {
		t.Fatal("cross-process publication incomplete", err)
	}
}
