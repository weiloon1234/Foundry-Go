// Package sourcemap reads and prunes private, manifest-owned release artifacts.
// It exposes no HTTP route, uploader, fetcher or background worker. Applications
// authorize reads and keep Directory outside their public tree. Build publishers
// must coordinate with pruning and publish immutable release directories: filesystem
// metadata checks detect changes but cannot make deletion atomic with outside writers.
//
// The compatible build layout is Directory/release/portal/manifest.json plus map
// files in that same portal directory. The manifest is JSON with string "release"
// and "portal" fields and an "assets" object mapping "_assets/basename.js" to a
// single map filename. Segments are 1..128 ASCII letters, digits, dots, underscores
// or hyphens, start with a letter or digit, and never contain "..". Maps are opaque
// bytes; this package neither parses nor rewrites their source content.
package sourcemap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

const (
	manifestBytes = 128 << 10
	maxReleases   = 1024
	maxPortals    = 32
	maxFiles      = 1024
	maxInventory  = 65536
)

// Asset is an exact portal/_assets/basename path. Read validates even cast values.
type Asset string

// ParseAsset validates a literal asset path without URL decoding or normalization.
func ParseAsset(text string) (Asset, error) {
	parts := strings.Split(text, "/")
	if len(parts) != 3 || !validSegment(parts[0]) || parts[1] != "_assets" || !validSegment(parts[2]) {
		return "", invalid("invalid private source map asset")
	}
	return Asset(text), nil
}

// Config declares a private build directory and explicit bounds. Empty Directory
// disables filesystem access. MaxBytes is 1..10 MiB; Retention is 1h..30d;
// StorageBytes is MaxBytes..1 GiB. Portals contains 1..32 distinct segments.
// Clock is required, borrowed, and invoked only by Prune. Release is protected
// from pruning; historical releases remain readable.
type Config struct {
	Directory    string
	Release      telemetry.Release
	Portals      []telemetry.Portal
	MaxBytes     int
	Retention    time.Duration
	StorageBytes int64
	Clock        clock.Clock
}

// Map owns a fresh byte slice containing the build's opaque source map.
type Map struct {
	Release telemetry.Release
	Asset   Asset
	Data    []byte
}

// PruneResult counts completely removed releases and their inventoried bytes.
// An error can accompany partial progress; never blindly retry a failed deletion.
type PruneResult struct {
	Releases int
	Bytes    int64
}

// Store retains an immutable configuration snapshot and no open files.
// Its zero value is unusable; construct one with New.
type Store struct{ config Config }

// New validates and snapshots configuration without filesystem or clock I/O.
func New(config Config) (*Store, error) {
	if !validSegment(string(config.Release)) || strings.ContainsRune(config.Directory, 0) || config.MaxBytes < 1 || config.MaxBytes > 10<<20 || config.Retention < time.Hour || config.Retention > 30*24*time.Hour || config.StorageBytes < int64(config.MaxBytes) || config.StorageBytes > 1<<30 || len(config.Portals) == 0 || len(config.Portals) > maxPortals || config.Clock == nil {
		return nil, invalid("invalid private source map configuration")
	}
	v := reflect.ValueOf(config.Clock)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan:
		if v.IsNil() {
			return nil, invalid("private source map clock is required")
		}
	}
	seen := make(map[telemetry.Portal]bool, len(config.Portals))
	for _, portal := range config.Portals {
		if !validSegment(string(portal)) || seen[portal] {
			return nil, invalid("invalid private source map portals")
		}
		seen[portal] = true
	}
	config.Portals = slices.Clone(config.Portals)
	return &Store{config: config}, nil
}

// Read returns a manifest-selected map from a configured portal. Missing artifacts
// retain os.ErrNotExist; malformed or unsafe artifacts return framework faults.
// Reads are bounded to 128 KiB for manifests and MaxBytes for maps, with context
// checks between reads. Cancellation cannot interrupt an in-flight OS operation.
func (s *Store) Read(ctx context.Context, release telemetry.Release, asset Asset) (out Map, err error) {
	defer func() { err = safeerror.Prefix("private source map read", err) }()
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if s == nil || s.config.Clock == nil {
		return out, invalid("private source map store is uninitialized")
	}
	if !validSegment(string(release)) {
		return out, invalid("invalid private source map release")
	}
	if _, err = ParseAsset(string(asset)); err != nil {
		return out, err
	}
	parts := strings.Split(string(asset), "/")
	if !slices.Contains(s.config.Portals, telemetry.Portal(parts[0])) {
		return out, invalid("unknown private source map portal")
	}
	if s.config.Directory == "" {
		return out, os.ErrNotExist
	}
	root, err := os.OpenRoot(s.config.Directory)
	if err != nil {
		return out, err
	}
	defer root.Close()
	rel, err := openDirectory(root, string(release))
	if err != nil {
		return out, err
	}
	defer rel.Close()
	portal, err := openDirectory(rel, parts[0])
	if err != nil {
		return out, err
	}
	defer portal.Close()
	manifest, err := readManifest(ctx, portal, string(release), parts[0])
	if err != nil {
		return out, err
	}
	name, ok := manifest.Assets[parts[1]+"/"+parts[2]]
	if !ok {
		return out, os.ErrNotExist
	}
	data, err := readPrivate(ctx, portal, name, s.config.MaxBytes)
	if err != nil {
		return out, err
	}
	return Map{Release: release, Asset: asset, Data: data}, nil
}

type sourceMapManifest struct {
	Release string            `json:"release"`
	Portal  string            `json:"portal"`
	Assets  map[string]string `json:"assets"`
}

func readManifest(ctx context.Context, root *os.Root, release, portal string) (sourceMapManifest, error) {
	var manifest sourceMapManifest
	data, err := readPrivate(ctx, root, "manifest.json", manifestBytes)
	if err != nil {
		return manifest, err
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.Release != release || manifest.Portal != portal || manifest.Assets == nil || len(manifest.Assets) > maxFiles-1 {
		return manifest, invalid("invalid private source map manifest")
	}
	for asset, file := range manifest.Assets {
		if _, err := ParseAsset(portal + "/" + asset); err != nil || !validSegment(file) || file == "manifest.json" {
			return manifest, invalid("invalid private source map manifest entry")
		}
	}
	return manifest, nil
}

func validSegment(text string) bool {
	if len(text) == 0 || len(text) > 128 || strings.Contains(text, "..") {
		return false
	}
	for i := range len(text) {
		c := text[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == '-') {
			continue
		}
		return false
	}
	return true
}

func invalid(message string) error { return safeerror.NewFault(fault.Invalid, message) }
func changed() error {
	return safeerror.NewFault(fault.Conflict, "private source map artifact changed")
}
func inventoryExceeded() error {
	return safeerror.NewFault(fault.Conflict, "private source map inventory exceeds bound")
}
func missing(err error) bool { return errors.Is(err, os.ErrNotExist) }
