package generate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

const manifestName = frameworkinfo.GenerationManifest
const lockName = ".foundry-generate.lock"

var outputName = regexp.MustCompile(`^[\pL\pN_]+_foundry\.gen\.go$`)

type manifest struct {
	Version      int                          `json:"version"`
	Files        map[string]string            `json:"files"`
	Distribution *pluginmanifest.Distribution `json:"distribution,omitempty"`
}
type oldFile struct {
	exists bool
	data   []byte
	mode   os.FileMode
}
type writePlan struct {
	changes      map[string][]byte
	before       map[string]oldFile
	guards       []string
	scaffold     bool
	fieldNotes   map[string]bool
	artifacts    bool
	distribution *pluginmanifest.Distribution
	newDirs      []string
}

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func validDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func readFile(path string) (oldFile, error) {
	return readWithin(filepath.Dir(path), filepath.Base(path))
}
func decodeManifest(data []byte) (manifest, error) {
	return decodeManifestPolicy(data, goOutputPolicy)
}

func decodeManifestPolicy(data []byte, policy outputPolicy) (manifest, error) {
	var m manifest
	if err := decodePublicationJSON(data, &m); err != nil {
		return m, fmt.Errorf("invalid generation manifest: %w", err)
	}
	if m.Version != policy.version || m.Files == nil {
		return m, fmt.Errorf("unsupported generation manifest")
	}
	if policy.distribution == nil && m.Distribution != nil {
		return m, fmt.Errorf("unexpected plugin distribution ownership")
	}
	if policy.distribution != nil {
		if len(m.Files) > MaxDistributionFiles {
			return m, fmt.Errorf("plugin ownership manifest exceeds its file bound")
		}
		if m.Distribution == nil || m.Distribution.Validate() != nil || !m.Distribution.SameOwner(*policy.distribution) {
			return m, fmt.Errorf("plugin distribution belongs to a different owner")
		}
	}
	for name, digest := range m.Files {
		if !policy.name(name) || (policy.distribution == nil && (filepath.Base(name) != name || strings.ContainsAny(name, "/\\"))) || !validDigest(digest) {
			return m, fmt.Errorf("generation manifest contains an invalid owned-file entry")
		}
	}
	return m, nil
}

func planWrite(p *packageInput, outputs map[string][]byte) (writePlan, error) {
	return planWritePolicy(p, outputs, goOutputPolicy)
}

func planWritePolicy(p *packageInput, outputs map[string][]byte, policy outputPolicy) (writePlan, error) {
	plan := writePlan{changes: make(map[string][]byte), before: make(map[string]oldFile), artifacts: policy.version == 2, distribution: policy.distribution}
	oldManifest, err := readFile(filepath.Join(p.dir, manifestName))
	if err != nil {
		return plan, err
	}
	owned := manifest{Version: policy.version, Files: make(map[string]string), Distribution: policy.distribution}
	if oldManifest.exists {
		owned, err = decodeManifestPolicy(oldManifest.data, policy)
		if err != nil {
			return plan, err
		}
		if policy.distribution != nil {
			order, err := policy.distribution.Release.Compare(owned.Distribution.Release)
			if err != nil || order < 0 {
				return plan, fmt.Errorf("cannot publish an older plugin distribution release")
			}
		}
	}
	for name := range p.previous {
		if _, ok := owned.Files[name]; !ok {
			return plan, fmt.Errorf("unowned generated file %s is not recorded in the manifest; review its ownership before generation", name)
		}
	}
	next := manifest{Version: policy.version, Files: make(map[string]string), Distribution: policy.distribution}
	for name, data := range outputs {
		if !policy.name(name) || !policy.content(name, data) || len(data) > 8<<20 {
			return plan, fmt.Errorf("invalid generated filename %s", name)
		}
		next.Files[name] = hash(data)
	}
	all := make(map[string]bool)
	for name := range owned.Files {
		all[name] = true
	}
	for name := range outputs {
		all[name] = true
	}
	previousBytes := 0
	for _, name := range sortedNames(all) {
		old, err := readWithin(p.dir, name)
		if err != nil {
			return plan, err
		}
		previousBytes += len(old.data)
		if policy.distribution != nil && previousBytes > MaxDistributionBytes {
			return plan, fmt.Errorf("owned plugin distribution exceeds its byte bound")
		}
		plan.before[name] = old
		if old.exists {
			digest, ours := owned.Files[name]
			if !ours || !policy.content(name, old.data) {
				return plan, fmt.Errorf("refusing to overwrite unowned file %s", name)
			}
			if hash(old.data) != digest {
				return plan, fmt.Errorf("owned output %s was edited outside the generator; restore or review it before regeneration", name)
			}
		}
		data, keep := outputs[name]
		if keep {
			if !old.exists || !bytes.Equal(data, old.data) {
				plan.changes[name] = data
			}
		} else if old.exists {
			plan.changes[name] = nil
		}
	}
	if len(outputs) == 0 && !oldManifest.exists {
		return plan, nil
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return plan, err
	}
	data = append(data, '\n')
	plan.before[manifestName] = oldManifest
	if !bytes.Equal(data, oldManifest.data) || (policy.distribution != nil && len(plan.changes) > 0) {
		plan.changes[manifestName] = data
	}
	return plan, nil
}

func publishWithRename(ctx context.Context, p *packageInput, plan writePlan, rename func(string, string) error) (result error) {
	return publishBatch(ctx, p.dir, []packageWrite{{p, plan}}, nil, rename)
}

func checkInput(p *packageInput, plan writePlan) error {
	if err := rejectAncestorPublication(p.dir); err != nil {
		return err
	}
	if err := checkDistributionChildren(p.dir, plan); err != nil {
		return err
	}
	// Recheck every handwritten input and publication target after acquiring the
	// lock so another generation or concurrent source edit cannot be overwritten.
	if p.goNames != nil {
		names, err := goSourceNames(p.dir)
		if err != nil {
			return err
		}
		if !slices.Equal(names, p.goNames) {
			return fmt.Errorf("Go source file set changed during generation; retry")
		}
	}
	for _, file := range p.files {
		current, err := os.ReadFile(filepath.Join(p.dir, file.name))
		if err != nil {
			return err
		}
		if !bytes.Equal(current, file.data) {
			return fmt.Errorf("source %s changed during generation; retry", file.name)
		}
	}
	for name, old := range plan.before {
		current, err := readWithin(p.dir, name)
		if err != nil {
			return err
		}
		if state(current) != state(old) {
			return fmt.Errorf("target %s changed during generation; retry", name)
		}
	}
	return nil
}

func publishBatch(ctx context.Context, dir string, writes []packageWrite, checkGraph func() error, rename func(string, string) error) error {
	plan, err := combineWrites(dir, writes)
	if err != nil {
		return err
	}
	release, err := acquireGuards(dir, plan.guards)
	if err != nil {
		return err
	}
	defer release()
	for _, write := range writes {
		if err := checkInput(write.input, write.plan); err != nil {
			return err
		}
	}
	if checkGraph != nil {
		if err := checkGraph(); err != nil {
			return err
		}
	}
	if rename == nil {
		var close func() error
		if plan.scaffold {
			rename, close, err = confinedCreate(dir)
		} else {
			rename, close, err = confinedRename(dir)
		}
		if err != nil {
			return err
		}
		defer close()
	}
	lock := filepath.Join(dir, lockName)
	if err := prepareStaging(dir, plan); err != nil {
		return err
	}
	var applied []string
	rollback := func(cause error) error {
		failures := []error{cause}
		for i := len(applied) - 1; i >= 0; i-- {
			name := applied[i]
			old := plan.before[name]
			current, err := readWithin(dir, name)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			expected := oldFile{}
			if data := plan.changes[name]; data != nil {
				mode := os.FileMode(0644)
				if old.exists {
					mode = old.mode
				}
				expected = oldFile{true, data, mode}
			}
			if state(current) != state(expected) {
				failures = append(failures, fmt.Errorf("rollback refuses changed target %s; backups retained", name))
				continue
			}
			err = restoreFile(dir, name, old, rename)
			if err != nil {
				failures = append(failures, fmt.Errorf("restore %s: %w; backups retained at %s", name, err, lock))
			}
		}
		if len(failures) == 1 {
			if err := syncDistributionDirectories(dir, plan.newDirs); err != nil {
				failures = append(failures, err)
			} else if err := syncGuards(dir, plan.guards); err != nil {
				failures = append(failures, err)
			} else if err := discardStaging(dir); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	if err := createDistributionDirectories(dir, plan); err != nil {
		return rollback(err)
	}
	for _, name := range publicationOrder(plan.changes) {
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		current, err := readWithin(dir, name)
		if err != nil {
			return rollback(err)
		}
		if state(current) != state(plan.before[name]) {
			return rollback(fmt.Errorf("target %s changed before publication", name))
		}
		if plan.changes[name] == nil {
			err = removeWithin(dir, name)
		} else {
			err = rename(filepath.Join(lock, stageName("new", name)), filepath.Join(dir, name))
		}
		if err != nil {
			return rollback(fmt.Errorf("publish %s: %w", name, err))
		}
		applied = append(applied, name)
	}
	if err := syncDistributionDirectories(dir, plan.newDirs); err != nil {
		return rollback(err)
	}
	if err := syncGuards(dir, plan.guards); err != nil {
		return rollback(err)
	}
	return discardStaging(dir)
}
