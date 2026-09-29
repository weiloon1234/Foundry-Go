package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

// ArtifactHeader is shared with language renderers. JSON outputs are identified
// by the versioned ownership manifest and digest, without invalid JSON comments.
const ArtifactHeader = generatedHeader

// MaxArtifactBytes bounds one published client artifact. A client embeds a
// manifest of up to 16 MiB plus its declarations, so it exceeds the Go limit.
const MaxArtifactBytes = 48 << 20

// maxGoOutputBytes bounds one generated Go file.
const maxGoOutputBytes = 8 << 20

type outputPolicy struct {
	version      int
	name         func(string) bool
	content      func(string, []byte) bool
	maxBytes     int
	distribution *pluginmanifest.Distribution
}

var artifactName = regexp.MustCompile(`^[a-zA-Z0-9_]+_foundry\.gen\.(ts|json)$`)
var goOutputPolicy = outputPolicy{version: 1, name: outputName.MatchString, maxBytes: maxGoOutputBytes, content: func(_ string, data []byte) bool { return bytes.HasPrefix(data, []byte(generatedHeader+"\n")) }}
var artifactOutputPolicy = outputPolicy{version: 2, name: artifactName.MatchString, maxBytes: MaxArtifactBytes, content: func(name string, data []byte) bool {
	if strings.HasSuffix(name, ".json") {
		return json.Valid(data)
	}
	return bytes.HasPrefix(data, []byte(ArtifactHeader+"\n"))
}}

// PublishArtifacts reuses the Go publisher's planning, ownership, read-only
// checks, guarded publication, rollback and crash recovery. It neither loads
// Go packages nor invokes a compiler. The existing output directory belongs to
// one artifact family; Go-owned and client-owned manifests cannot be mixed.
func PublishArtifacts(ctx context.Context, dir string, outputs map[string][]byte, check bool) (Report, error) {
	var report Report
	absolute, err := artifactRoot(ctx, dir, check)
	if err != nil {
		return report, err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return report, err
	}
	input := &packageInput{dir: absolute, previous: make(map[string][]byte)}
	for _, entry := range entries {
		if artifactName.MatchString(entry.Name()) {
			input.previous[entry.Name()] = nil
		}
	}
	owned := make(map[string][]byte, len(outputs))
	for name, data := range outputs {
		owned[name] = bytes.Clone(data)
	}
	plan, err := planWritePolicy(input, owned, artifactOutputPolicy)
	if err != nil {
		return report, err
	}
	for _, name := range sortedNames(plan.changes) {
		if name == manifestName {
			continue
		}
		if plan.changes[name] == nil {
			report.Removed = append(report.Removed, name)
		} else {
			report.Written = append(report.Written, name)
		}
	}
	if len(plan.changes) == 0 {
		return report, ctx.Err()
	}
	if check {
		return report, fmt.Errorf("client artifacts are stale; regenerate with the same manifest and options")
	}
	if err := publishBatch(ctx, absolute, []packageWrite{{input, plan}}, nil, nil); err != nil {
		return Report{}, err
	}
	return report, nil
}

func artifactRoot(ctx context.Context, dir string, check bool) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("artifact publication requires a context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if err := rejectAncestorPublication(absolute); err != nil {
		return "", err
	}
	if _, err := os.Lstat(filepath.Join(absolute, lockName)); err == nil {
		if check {
			return "", fmt.Errorf("artifact publication is active or interrupted; recover before checking")
		}
		if _, err := Recover(ctx, absolute); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return absolute, nil
}
