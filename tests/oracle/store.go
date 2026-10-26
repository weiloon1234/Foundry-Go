// Package oracle owns the frozen legacy evidence format and its integrity checks.
package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Version = 1

type Provenance struct {
	WorkingDirectory string           `json:"working_directory"`
	Build            BuildEnvironment `json:"build"`
	Commit           string           `json:"commit"`
	Sources          string           `json:"source_manifest_sha256"`
	Command          []string         `json:"command"`
	Toolchain        string           `json:"toolchain"`
	Platform         string           `json:"platform"`
}
type Record struct {
	Package    string     `json:"package,omitempty"`
	Site       string     `json:"site"`
	Kind       string     `json:"kind"`
	Transcript string     `json:"transcript"`
	Provenance Provenance `json:"provenance"`
}
type Bundle struct {
	Version int               `json:"version"`
	Sources map[string]string `json:"source_sha256"`
	Records map[string]Record `json:"records"`
}
type State struct {
	Version int    `json:"version"`
	Phase   string `json:"phase"`
	SHA256  string `json:"bundle_sha256,omitempty"`
	Command string `json:"authorization_command,omitempty"`
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func ReadJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func JSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	return append(data, '\n'), err
}
func Validate(b Bundle) error {
	if b.Version != Version || len(b.Records) == 0 {
		return fmt.Errorf("empty or unsupported oracle bundle")
	}
	sourceData, err := JSON(b.Sources)
	if err != nil {
		return err
	}
	if len(b.Sources) == 0 {
		return fmt.Errorf("missing source manifest")
	}
	for path, hash := range b.Sources {
		if path == "" || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") || len(hash) != 64 || !hexadecimal(hash) {
			return fmt.Errorf("invalid source hash %q", path)
		}
	}
	for id, r := range b.Records {
		p := r.Provenance
		if r.Kind == "golden" {
			if r.Package == "" {
				return fmt.Errorf("record %q lacks package identity", id)
			}
			if _, err := recordTest(id, r.Package, r.Site); err != nil {
				return err
			}
		}
		if id == "" || r.Site == "" || r.Transcript == "" || (r.Kind != "golden" && r.Kind != "footprint") {
			return fmt.Errorf("invalid record %q", id)
		}
		if len(p.Commit) != 40 || !hexadecimal(p.Commit) || p.Sources != Digest(sourceData) || len(p.Command) == 0 || p.Toolchain == "" || p.Platform == "" || p.Build.Validate(p.WorkingDirectory) != nil {
			return fmt.Errorf("record %q lacks provenance", id)
		}
	}
	return nil
}
func hexadecimal(s string) bool { _, err := hex.DecodeString(s); return err == nil }

// Load checks the separate publication receipt before decoding evidence. A missing
// bundle is allowed only while the checked-in receipt explicitly says pending.
func Load(dir string) (Bundle, error) {
	var state State
	if err := ReadJSON(filepath.Join(dir, "state.json"), &state); err != nil {
		return Bundle{}, err
	}
	if state.Version != Version {
		return Bundle{}, fmt.Errorf("unsupported oracle state")
	}
	data, err := os.ReadFile(filepath.Join(dir, "records.json"))
	if state.Phase == "pending" && os.IsNotExist(err) && state.SHA256 == "" && state.Command == "" {
		return Bundle{Version: Version}, nil
	}
	if err != nil {
		return Bundle{}, err
	}
	if state.Phase != "frozen" || state.SHA256 != Digest(data) || (state.Command != "go run ./cmd/freeze-oracles --freeze" && state.Command != "go run ./cmd/freeze-oracles --refreeze") {
		return Bundle{}, fmt.Errorf("oracle evidence changed without explicit freeze/refreeze publication")
	}
	var b Bundle
	if err = json.Unmarshal(data, &b); err != nil {
		return Bundle{}, err
	}
	return b, Validate(b)
}

// Publish publishes a reviewed legacy capture bundle.
func Publish(dir string, b Bundle, refreeze bool) error {
	current, err := Load(dir)
	if err != nil {
		return err
	}
	if (len(current.Records) > 0) != refreeze {
		return fmt.Errorf("use --freeze for pending evidence, --refreeze for existing evidence")
	}
	if err = Validate(b); err != nil {
		return err
	}
	data, err := JSON(b)
	if err != nil {
		return err
	}
	action := "--freeze"
	if refreeze {
		action = "--refreeze"
	}
	receipt, err := JSON(State{Version: Version, Phase: "frozen", SHA256: Digest(data), Command: "go run ./cmd/freeze-oracles " + action})
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name string
		data []byte
	}{{"records.json", data}, {"state.json", receipt}} {
		f, err := os.CreateTemp(dir, ".oracle-*")
		if err != nil {
			return err
		}
		name := f.Name()
		_, err = f.Write(item.data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(name, filepath.Join(dir, item.name))
		}
		if err != nil {
			os.Remove(name)
			return err
		}
	}
	return nil
}
