package doctor

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeDoctorReadsSelectedModuleWithoutBuildingIt(t *testing.T) {
	testkit.TrackExternalInputs(t)
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	report, err := Inspect(t.Context(), Options{Dir: root, Go: tool, Gopls: os.Getenv("FOUNDRY_TEST_GOPLS"), RequireGopls: os.Getenv("FOUNDRY_TEST_GOPLS") != "", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) < 4 {
		t.Fatal("native doctor omitted module checks")
	}
}

func TestDoctorProcessTimeoutAndOutputBound(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"hang", "large", "ok"} {
		t.Run(mode, func(t *testing.T) {
			// Race-instrumented binaries allow a grace period before normal exit.
			// Keep the hanging process's deadline short while allowing that grace
			// for success/output-limit cases; production options are unchanged.
			timeout := 3 * time.Second
			if mode == "hang" {
				timeout = time.Second
			}
			started := time.Now()
			data, err := run(t.Context(), Options{Dir: t.TempDir(), Timeout: timeout}, binary, "-test.run=^TestDoctorProcessHelper$", "--", mode)
			if time.Since(started) > timeout+3*time.Second {
				t.Fatal("doctor process exceeded timeout and wait bound")
			}
			if mode == "ok" {
				if err != nil || !strings.Contains(string(data), "doctor-helper") {
					t.Fatal("normal process result lost", err)
				}
			} else if err == nil {
				t.Fatal("invalid process result passed")
			}
			if mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("process deadline lost", err)
			}
		})
	}
}
func TestDoctorProcessHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "hang":
		time.Sleep(time.Minute)
	case "large":
		fmt.Print(strings.Repeat("x", maxOutputBytes+1))
	case "ok":
		fmt.Print("doctor-helper")
	}
}
