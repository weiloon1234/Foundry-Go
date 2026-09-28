// Package testinputs fingerprints external child-test inputs which Go's ordinary
// test cache cannot track across module roots. It reads sources and selected
// tools, never application environment files, credential stores or private caches.
package testinputs

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

const Variable = "FOUNDRY_TEST_INPUTS"
const maxFileBytes int64 = 256 << 20
const maxTotalBytes int64 = 2 << 30
const maxFiles = 100000

type Tool struct {
	Name, Path string
	Tree       bool
}
type fingerprint struct {
	ctx    context.Context
	hash   hash.Hash
	bytes  int64
	files  int
	buffer [64 << 10]byte
}

// Compute is deterministic across timestamp-only changes. Source symlinks are
// rejected instead of silently omitting outside-workspace child dependencies.
// Tools are explicit caller-selected files/trees and may live outside the repo.
func Compute(ctx context.Context, root string, identity []string, tools []Tool) (string, error) {
	if ctx == nil {
		return "", errors.New("test inputs require a context")
	}
	canonical, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	canonical, err = filepath.EvalSymlinks(canonical)
	if err != nil {
		return "", err
	}
	f := fingerprint{ctx: ctx, hash: sha256.New()}
	f.add("foundry-external-inputs-v1")
	f.add(canonical)
	for _, item := range identity {
		f.add(item)
	}
	if err := f.tree("source", canonical); err != nil {
		return "", err
	}
	tools = slices.Clone(tools)
	slices.SortFunc(tools, func(a, b Tool) int { return strings.Compare(a.Name, b.Name) })
	seen := make(map[string]bool)
	for _, tool := range tools {
		if tool.Name == "" || seen[tool.Name] {
			return "", errors.New("test input tool names must be unique")
		}
		seen[tool.Name] = true
		f.add(tool.Name)
		if tool.Path == "" {
			f.add("not-selected")
			continue
		}
		path, err := filepath.Abs(tool.Path)
		if err != nil {
			return "", err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", errors.New("selected test input tool is unavailable")
		}
		f.add(path)
		if tool.Tree {
			if err := f.tree("tool:"+tool.Name, path); err != nil {
				return "", err
			}
		} else {
			if err := f.file("tool:"+tool.Name, path); err != nil {
				return "", err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(f.hash.Sum(nil)), nil
}
func (f *fingerprint) add(value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = f.hash.Write(size[:])
	_, _ = io.WriteString(f.hash, value)
}

func sourceFile(name string) bool {
	if strings.HasPrefix(name, ".") && name != frameworkinfo.GenerationManifest {
		return false
	}
	switch name {
	case "Makefile", "go.mod", "go.sum":
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".mod", ".sum", ".json", ".js", ".mjs", ".ts", ".md", ".sql", ".toml", ".yaml", ".yml", ".sh", ".txt", ".csv", ".html":
		return true
	}
	return false
}
func (f *fingerprint) tree(label, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("test input source tree is unreadable")
		}
		if err := f.ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if strings.HasPrefix(entry.Name(), ".") && entry.Name() != frameworkinfo.GenerationManifest || entry.Name() == "node_modules" || entry.Name() == "bin" || entry.Name() == "coverage" {
				return nil
			}
			return errors.New("test input source symlinks are unsupported; use explicit source files")
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "bin" || entry.Name() == "coverage") {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceFile(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return f.file(label+":"+filepath.ToSlash(relative), path)
	})
}
func (f *fingerprint) file(label, path string) (err error) {
	if err := f.ctx.Err(); err != nil {
		return err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return errors.New("test input must be a readable regular file")
	}
	if before.Size() > maxFileBytes || before.Size() > maxTotalBytes-f.bytes || f.files >= maxFiles {
		return errors.New("test input fingerprint exceeds its resource bound")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("test input cannot be opened")
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return errors.New("test input changed while opening")
	}
	f.add(label)
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(before.Size()))
	_, _ = f.hash.Write(size[:])
	buffer := f.buffer[:]
	reader := io.LimitReader(file, before.Size()+1)
	var read int64
	for {
		if err := f.ctx.Err(); err != nil {
			return err
		}
		n, readErr := reader.Read(buffer)
		read += int64(n)
		if read > before.Size() {
			return errors.New("test input grew during fingerprint")
		}
		if n > 0 {
			_, _ = f.hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return errors.New("test input read failed")
		}
	}
	after, err := file.Stat()
	if err != nil || read != before.Size() || after.Size() != before.Size() || !before.ModTime().Equal(after.ModTime()) || after.Mode() != before.Mode() {
		return errors.New("test input changed during fingerprint")
	}
	f.bytes += read
	f.files++
	return nil
}
