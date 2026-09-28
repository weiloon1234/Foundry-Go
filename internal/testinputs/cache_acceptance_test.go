package testinputs

import (
	"context"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFingerprintInvalidatesGoCacheForOutsideModuleChildInputs(t *testing.T) {
	// This acceptance itself executes the selected external Go tool. Its parent
	// cache must observe the same key whose behavior it verifies in the child.
	_ = os.Getenv(Variable)
	root := t.TempDir()
	consumer := filepath.Join(root, "consumer")
	goVersion := strings.TrimPrefix(version.Lang(runtime.Version()), "go")
	if goVersion == "" {
		t.Fatal("Go language version is unavailable")
	}
	put(t, root, "consumer/go.mod", "module cache.test/consumer\n\ngo "+goVersion+"\n")
	put(t, root, "outside/input.txt", "first")
	put(t, root, "consumer/cache_test.go", fmt.Sprintf(`package consumer
import("os";"testing")
func TestInput(t *testing.T){
 _=os.Getenv(%s)
 data,err:=os.ReadFile(%s);if err!=nil{t.Fatal(err)}
 t.Log("seen:"+string(data))
}
`, strconv.Quote(Variable), strconv.Quote(filepath.Join(root, "outside/input.txt"))))
	check := func(digest string) (string, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "go", "test", "-v", "./...")
		command.Dir = consumer
		command.WaitDelay = time.Second
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			switch name {
			case Variable, "GOWORK", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "GOFLAGS":
				continue
			}
			command.Env = append(command.Env, entry)
		}
		command.Env = append(command.Env, Variable+"="+digest, "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=readonly")
		output, err := command.CombinedOutput()
		return string(output), err
	}
	first := fingerprintOf(t, root)
	output, err := check(first)
	if err != nil || !strings.Contains(output, "seen:first") {
		t.Fatal("initial cache fixture failed", err, output)
	}
	output, err = check(first)
	if err != nil || !strings.Contains(output, "(cached)") {
		t.Fatal("unchanged source did not retain ordinary test caching", err, output)
	}
	put(t, root, "outside/input.txt", "second")
	second := fingerprintOf(t, root)
	if second == first {
		t.Fatal("external module input did not change fingerprint")
	}
	output, err = check(second)
	if err != nil || strings.Contains(output, "(cached)") || !strings.Contains(output, "seen:second") {
		t.Fatal("external source returned stale parent test success", err, output)
	}
}
