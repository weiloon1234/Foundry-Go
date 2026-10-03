package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

type sourceFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type sourcePolicy struct{ generationManifest string }

// Keep the generator's ownership filename at its existing source of truth.
// The release tool is an independent module and must not import an unreleased
// framework through a local replacement simply to read this literal constant.
func policyFor(root string) (sourcePolicy, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/frameworkinfo/module.go"), nil, 0)
	if err != nil {
		return sourcePolicy{}, err
	}
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			if len(value.Names) != 1 || value.Names[0].Name != "GenerationManifest" || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				break
			}
			name, err := strconv.Unquote(literal.Value)
			if err != nil || filepath.Base(name) != name || !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
				break
			}
			return sourcePolicy{generationManifest: name}, nil
		}
	}
	return sourcePolicy{}, fmt.Errorf("cannot read generator ownership filename from frameworkinfo")
}

func hasDirectoryPrefix(path, directory string) bool {
	return strings.HasPrefix(path, directory+string(filepath.Separator))
}
func ignoredDirectory(name string) bool {
	return strings.HasPrefix(name, ".") || name == "bin" || name == "node_modules" || name == "vendor" || name == "coverage" || name == "__pycache__"
}
func (policy sourcePolicy) sourceName(path string) bool {
	name := filepath.Base(path)
	if name == policy.generationManifest {
		return true
	}
	if strings.HasPrefix(name, ".") || name == "AGENTS.md" {
		return false
	}
	switch name {
	case "go.mod", "go.sum", "Makefile", "LICENSE", "NOTICE", "COPYING":
		return true
	}
	switch filepath.Ext(name) {
	case ".go", ".md", ".lua", ".json", ".ts", ".sh", ".version", ".html", ".css", ".mjs", ".tab", ".py":
		return true
	}
	// Reviewed imaging codec fixtures and their upstream patent notice. Keep
	// binary allowance scoped to these test directories, never arbitrary assets.
	relative := filepath.ToSlash(path)
	if strings.HasPrefix(relative, "imaging/testdata/avif/") {
		return filepath.Ext(name) == ".avif" || name == "PATENTS"
	}
	if strings.HasPrefix(relative, "imaging/testdata/webp/") {
		return filepath.Ext(name) == ".png" || filepath.Ext(name) == ".webp"
	}
	if strings.HasPrefix(relative, "imaging/testdata/native/") {
		switch filepath.Ext(name) {
		case ".png", ".jpg", ".icc", ".jxl":
			return true
		}
	}
	// Go's checked-in fuzz corpus uses extensionless hexadecimal filenames.
	return strings.Contains(filepath.ToSlash(path), "testdata/fuzz/")
}

// walkSource is also the private-artifact boundary: no hidden files, credentials,
// dependency trees, links, nested modules, or unknown file types are copied.
// Unexpected regular files fail explicitly instead of silently losing assets.
func (policy sourcePolicy) walkSource(root string, visit func(string, string, []byte) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			if !entry.IsDir() {
				return fmt.Errorf("source root must be a directory: %s", root)
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if _, err := os.Lstat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			} else if !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		if (strings.HasPrefix(entry.Name(), ".") && entry.Name() != policy.generationManifest) || entry.Name() == "AGENTS.md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release rejects irregular source %s", rel)
		}
		if !policy.sourceName(rel) {
			return fmt.Errorf("release file policy must explicitly allow %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return visit(path, rel, data)
	})
}

func fileRecord(path string, data []byte) sourceFile {
	sum := sha256.Sum256(data)
	return sourceFile{Path: filepath.ToSlash(path), Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func (policy sourcePolicy) copySource(source, destination string) ([]sourceFile, error) {
	if err := os.MkdirAll(destination, 0700); err != nil {
		return nil, err
	}
	var records []sourceFile
	err := policy.walkSource(source, func(_ string, rel string, data []byte) error {
		path := filepath.Join(destination, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		records = append(records, fileRecord(rel, data))
		return nil
	})
	return records, err
}
func (policy sourcePolicy) inventory(root string) ([]sourceFile, error) {
	var records []sourceFile
	err := policy.walkSource(root, func(_ string, rel string, data []byte) error {
		records = append(records, fileRecord(rel, data))
		return nil
	})
	return records, err
}

func (policy sourcePolicy) createArtifact(output, source, location, path, version, goVersion string) (artifact, error) {
	item := artifact{Path: path, Version: version, Go: goVersion, Source: location}
	escapedPath, err := module.EscapePath(path)
	if err != nil {
		return item, err
	}
	escapedVersion, err := module.EscapeVersion(version)
	if err != nil {
		return item, err
	}
	dir := filepath.Join(output, "proxy", filepath.FromSlash(escapedPath), "@v")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return item, err
	}
	zipPath := filepath.Join(dir, escapedVersion+".zip")
	file, err := os.OpenFile(zipPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return item, err
	}
	zipErr := modzip.CreateFromDir(file, module.Version{Path: path, Version: version}, source)
	closeErr := file.Close()
	if zipErr != nil {
		return item, zipErr
	}
	if closeErr != nil {
		return item, closeErr
	}
	if _, err := modzip.CheckZip(module.Version{Path: path, Version: version}, zipPath); err != nil {
		return item, err
	}
	item.Sum, err = dirhash.HashZip(zipPath, dirhash.Hash1)
	if err != nil {
		return item, err
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return item, err
	}
	item.SHA256 = fileRecord("", data).SHA256
	rel, err := filepath.Rel(output, zipPath)
	if err != nil {
		return item, err
	}
	item.Zip = filepath.ToSlash(rel)
	item.Files, err = policy.inventory(source)
	if err != nil {
		return item, err
	}
	mod, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		return item, err
	}
	if err := os.WriteFile(filepath.Join(dir, escapedVersion+".mod"), mod, 0600); err != nil {
		return item, err
	}
	// Stable candidate metadata and deterministic zip contents permit comparison.
	if err := writeJSON(filepath.Join(dir, escapedVersion+".info"), struct{ Version, Time string }{version, "1970-01-01T00:00:00Z"}); err != nil {
		return item, err
	}
	if err := os.WriteFile(filepath.Join(dir, "list"), []byte(version+"\n"), 0600); err != nil {
		return item, err
	}
	for _, name := range []string{escapedVersion + ".mod", escapedVersion + ".info", "list"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return item, err
		}
		rel, err := filepath.Rel(output, path)
		if err != nil {
			return item, err
		}
		item.ProxyFiles = append(item.ProxyFiles, fileRecord(rel, data))
	}
	return item, nil
}
