// Package assets distributes immutable plugin files through Foundry's shared
// guarded publisher. Publication is explicit and never part of application boot.
package assets

import (
	"context"
	"fmt"
	"sort"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

type ID string
type File struct {
	Path string
	Data []byte
}
type FileInfo struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}
type Report = generate.Report
type Recovery = generate.Recovery

const MaxFiles = generate.MaxDistributionFiles
const MaxBytes = generate.MaxDistributionBytes
const MaxFileBytes = generate.MaxDistributionFileBytes

// Bundle snapshots every source file. It is safe to share across applications;
// inspection returns copies and publication does not modify the bundle.
type Bundle struct {
	info  manifest.Distribution
	files map[string][]byte
}

func New(declaration manifest.Manifest, id ID, files ...File) (*Bundle, error) {
	if err := declaration.Validate(); err != nil {
		return nil, err
	}
	info := manifest.Distribution{Plugin: declaration.ID, Release: declaration.Version, Kind: manifest.Assets, Name: string(id)}
	if err := info.Validate(); err != nil {
		return nil, err
	}
	owned, err := Snapshot(files)
	if err != nil {
		return nil, err
	}
	return &Bundle{info, owned}, nil
}

// Snapshot validates all paths before any publication. Nil data means an empty
// file, never deletion. Omitted files are removed only when the publisher proves
// that they belong to this exact distribution and have not been manually edited.
func Snapshot(files []File) (map[string][]byte, error) {
	if len(files) > MaxFiles {
		return nil, fault.New(fault.Invalid, "plugin file count exceeds its bound")
	}
	outputs := make(map[string][]byte, len(files))
	for _, file := range files {
		if _, exists := outputs[file.Path]; exists {
			return nil, fault.New(fault.Duplicate, "plugin distribution repeats a file path")
		}
		outputs[file.Path] = file.Data
	}
	return generate.SnapshotDistribution(outputs)
}

func (b *Bundle) Info() manifest.Distribution {
	if b == nil {
		return manifest.Distribution{}
	}
	return b.info
}
func (b *Bundle) Files() []FileInfo {
	if b == nil {
		return nil
	}
	result := make([]FileInfo, 0, len(b.files))
	for name, data := range b.files {
		result = append(result, FileInfo{name, len(data)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

// Publish confines files to one existing selected output root. Different bundle
// owners, older releases, unowned files and edited owned files are refused. Check
// is read-only, including when recovery is pending. Failed publication can leave
// newly created empty directories; it never recursively deletes directories.
func (b *Bundle) Publish(ctx context.Context, dir string, check bool) (Report, error) {
	if b == nil || b.files == nil {
		return Report{}, fault.New(fault.Invalid, "uninitialized plugin assets")
	}
	return generate.PublishDistribution(ctx, dir, b.info, b.files, check)
}

// Recover is the shared publisher recovery entry point; the same operation is
// available through foundry generate --recover --dir <selected output root>.
func Recover(ctx context.Context, dir string) (Recovery, error) {
	if ctx == nil {
		return Recovery{}, fault.New(fault.Invalid, "nil plugin publication context")
	}
	return generate.Recover(ctx, dir)
}

var bundles = foundation.NewCollection[*Bundle]("plugin.assets")

// Register contributes an already snapshotted bundle under the captured plugin
// owner/release. It performs no publication or filesystem reads.
func Register(r *foundation.Registrar, bundle *Bundle) error {
	declaration, ok := r.Plugin()
	if !ok || bundle == nil || bundle.files == nil || bundle.info.Plugin != declaration.ID || bundle.info.Release != declaration.Version {
		return fault.New(fault.Invalid, "asset bundle differs from its plugin owner or release")
	}
	return foundation.Contribute(r, bundles, fmt.Sprintf("%q.%q", bundle.info.Plugin, bundle.info.Name), func(foundation.Resolver) (*Bundle, error) { return bundle, nil })
}

// Bundles returns immutable bundles registered in dependency order. Calling it
// on App.Services needs no boot or runtime infrastructure.
func Bundles(resolver foundation.Resolver) ([]*Bundle, error) {
	return foundation.Contributions(resolver, bundles)
}
