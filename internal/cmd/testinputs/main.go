// Command testinputs prints one shell/make-safe environment assignment after
// hashing complete child-test inputs. Failure prints no partial assignment.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/testinputs"
)

func main() {
	goTool := flag.String("go", "go", "Go executable selected by make")
	root := flag.String("root", ".", "framework source root")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "testinputs accepts flags only")
		os.Exit(1)
	}
	assignment, err := assignment(context.Background(), *root, *goTool)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not fingerprint external test inputs:", err)
		os.Exit(1)
	}
	fmt.Println(assignment)
}
func assignment(ctx context.Context, root, goTool string) (string, error) {
	identity := []string{runtime.Version(), runtime.GOOS, runtime.GOARCH}
	if info, ok := debug.ReadBuildInfo(); ok {
		data, err := json.Marshal(info)
		if err != nil {
			return "", err
		}
		identity = append(identity, string(data))
	}
	for _, name := range []string{"GOFLAGS", "GOEXPERIMENT", "CGO_ENABLED", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS", "CC", "CXX"} {
		identity = append(identity, name+"="+os.Getenv(name))
	}
	var tools []testinputs.Tool
	for _, selection := range []struct{ name, path string }{{"make-go", goTool}, {"child-go", "go"}, {"gopls", os.Getenv("FOUNDRY_TEST_GOPLS")}, {"node", os.Getenv("FOUNDRY_TEST_NODE")}} {
		path := selection.path
		if path != "" {
			var err error
			path, err = exec.LookPath(path)
			if err != nil {
				return "", fmt.Errorf("selected %s tool is unavailable", selection.name)
			}
		}
		tools = append(tools, testinputs.Tool{Name: selection.name, Path: path})
	}
	typescript := os.Getenv("FOUNDRY_TEST_TYPESCRIPT")
	if typescript != "" {
		path, err := filepath.Abs(typescript)
		if err != nil {
			return "", err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("selected TypeScript tool is unavailable")
		}
		if filepath.Base(path) != "tsc.js" || filepath.Base(filepath.Dir(path)) != "lib" {
			return "", fmt.Errorf("select the installed TypeScript package's lib/tsc.js for complete cache tracking")
		}
		packageRoot := filepath.Dir(filepath.Dir(path))
		data, err := packageMetadata(filepath.Join(packageRoot, "package.json"))
		if err != nil {
			return "", fmt.Errorf("selected TypeScript package metadata is unavailable")
		}
		var metadata struct{ Name string }
		if len(data) > 64<<10 || json.Unmarshal(data, &metadata) != nil || metadata.Name != "typescript" {
			return "", fmt.Errorf("selected TypeScript package identity is invalid")
		}
		tools = append(tools, testinputs.Tool{Name: "typescript-package", Path: packageRoot, Tree: true})
	} else {
		tools = append(tools, testinputs.Tool{Name: "typescript-package"})
	}
	digest, err := testinputs.Compute(ctx, root, identity, tools)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(digest, " \r\n\t") {
		return "", fmt.Errorf("invalid test input digest")
	}
	return testinputs.Variable + "=" + digest, nil
}

// Bound metadata before allocation, even when the selected package is malformed.
func packageMetadata(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, fmt.Errorf("invalid package metadata size")
	}
	return io.ReadAll(io.LimitReader(file, (64<<10)+1))
}
