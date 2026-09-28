package http

import (
	"io/fs"
	"maps"
	"path"
	"reflect"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// AssetPath is a relative slash-separated asset path. Empty identifies a mount
// root; one trailing slash identifies a directory. Download requires a file path.
type AssetPath string

func (p AssetPath) Validate() error { _, _, err := assetPathParts(string(p)); return err }

// AssetExtension is a lowercase suffix including its leading dot, such as .css.
type AssetExtension string

// AssetSource is a declared local directory or caller-owned filesystem.
// The zero value is invalid. Construct it with DirectoryAssets or FilesystemAssets.
type AssetSource struct {
	directory  string
	filesystem fs.FS
}

func DirectoryAssets(directory string) AssetSource { return AssetSource{directory: directory} }

// FilesystemAssets supports embed.FS and other concurrency-safe filesystems.
// The caller owns fsys; Foundry closes individual files. Only DirectoryAssets
// promises os.Root confinement. Files must implement io.ReadSeekCloser.
func FilesystemAssets(fsys fs.FS) AssetSource { return AssetSource{filesystem: fsys} }
func (s AssetSource) validate() error {
	if s.directory != "" {
		if s.filesystem != nil || strings.ContainsRune(s.directory, 0) || strings.TrimSpace(s.directory) != s.directory {
			return fault.New(fault.Invalid, "invalid asset directory")
		}
		return nil
	}
	if s.filesystem == nil {
		return fault.New(fault.Invalid, "assets require a source")
	}
	v := reflect.ValueOf(s.filesystem)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		if v.IsNil() {
			return fault.New(fault.Invalid, "assets require a non-nil filesystem")
		}
	}
	return nil
}

// AssetsConfig owns source, index, media and transfer policies. Start with
// DefaultAssetsConfig. Constructors snapshot Media; unknown suffixes use
// application/octet-stream, without consulting host MIME files or global state.
type AssetsConfig struct {
	Source       AssetSource
	Index        AssetPath
	Media        map[AssetExtension]MediaType
	CacheControl HeaderValue
	Limits       FileResponseLimits
	AllowHidden  bool
}

func DefaultAssetsConfig(source AssetSource) AssetsConfig {
	return AssetsConfig{Source: source, Index: "index.html", CacheControl: "no-cache", Limits: DefaultFileResponseLimits(), Media: map[AssetExtension]MediaType{
		".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
		".json": "application/json", ".map": "application/json", ".webmanifest": "application/manifest+json", ".txt": "text/plain; charset=utf-8", ".xml": "application/xml",
		".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/vnd.microsoft.icon",
		".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf", ".wasm": "application/wasm", ".pdf": "application/pdf",
	}}
}
func (c AssetsConfig) Validate() error {
	if err := c.Source.validate(); err != nil {
		return err
	}
	if err := c.Limits.Validate(); err != nil {
		return err
	}
	if err := c.CacheControl.Validate(); err != nil {
		return err
	}
	if c.Index != "" {
		if err := assetFilePath(c.Index); err != nil {
			return err
		}
		if strings.Contains(string(c.Index), "/") {
			return fault.New(fault.Invalid, "directory index must be one filename")
		}
		if !c.allowed(string(c.Index)) {
			return fault.New(fault.Invalid, "directory index is hidden by asset policy")
		}
	}
	if len(c.Media) > 64 {
		return fault.New(fault.Invalid, "asset media declarations exceed limit")
	}
	for extension, media := range c.Media {
		text := string(extension)
		if len(text) < 2 || len(text) > 32 || text[0] != '.' {
			return fault.New(fault.Invalid, "invalid asset extension")
		}
		for _, r := range text[1:] {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return fault.New(fault.Invalid, "asset extension must be lowercase")
			}
		}
		if err := media.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (c AssetsConfig) snapshot() AssetsConfig { c.Media = maps.Clone(c.Media); return c }
func (c AssetsConfig) media(name string) MediaType {
	if media, ok := c.Media[AssetExtension(strings.ToLower(path.Ext(name)))]; ok {
		return media
	}
	return "application/octet-stream"
}
func (c AssetsConfig) allowed(name string) bool {
	if c.AllowHidden {
		return true
	}
	for i, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") && !(i == 0 && part == ".well-known") {
			return false
		}
	}
	return true
}
func assetPathParts(text string) (string, bool, error) {
	trailing := strings.HasSuffix(text, "/")
	name := strings.TrimSuffix(text, "/")
	if strings.HasPrefix(text, "/") || len(text) > 4096 || strings.Contains(text, "\\") || name == "." || name != "" && !fs.ValidPath(name) {
		return "", false, fault.New(fault.Invalid, "invalid relative asset path")
	}
	if err := validateParameterText(name, true); err != nil {
		return "", false, err
	}
	return name, trailing, nil
}
func assetFilePath(name AssetPath) error {
	text, dir, err := assetPathParts(string(name))
	if err != nil {
		return err
	}
	if text == "" || dir {
		return fault.New(fault.Invalid, "asset file requires a non-directory path")
	}
	return nil
}
