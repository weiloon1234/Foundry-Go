package sourcemap

import (
	"context"
	"math"
	"os"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type privateFile struct {
	name string
	info os.FileInfo
}
type privatePortal struct {
	name  string
	info  os.FileInfo
	files []privateFile
}
type privateRelease struct {
	name     string
	info     os.FileInfo
	bytes    int64
	modified time.Time
	portals  []privatePortal
}

// Prune removes the oldest owned releases until retention and storage bounds are
// met, always preserving Config.Release. Unknown files/portals preserve an entire
// release. Missing roots are harmless. Malformed artifacts fail closed. Storage
// accounting includes only fully owned releases; unrelated files are not managed.
// Inventories allow 1024 root entries, 32 portals per release, 1024 files per
// portal, and 65536 total entries. Exceeding a bound fails before any deletion.
// Active-release cap excess returns a Conflict fault with any completed progress.
func (s *Store) Prune(ctx context.Context) (result PruneResult, err error) {
	defer func() { err = safeerror.Prefix("private source map prune", err) }()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if s == nil || s.config.Clock == nil {
		return result, invalid("private source map store is uninitialized")
	}
	if s.config.Directory == "" {
		return result, nil
	}
	now, err := temporal.Now(s.config.Clock)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	root, err := os.OpenRoot(s.config.Directory)
	if missing(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer root.Close()
	entries, err := directoryEntries(ctx, root, maxReleases)
	if err != nil {
		return result, err
	}
	budget := maxInventory - len(entries)
	var total int64
	var releases []privateRelease
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if !validSegment(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		release, owned, inspectErr := s.inspectRelease(ctx, root, entry.Name(), &budget)
		if inspectErr != nil {
			return result, inspectErr
		}
		if !owned {
			continue
		}
		if release.bytes > math.MaxInt64-total {
			return result, inventoryExceeded()
		}
		total += release.bytes
		releases = append(releases, release)
	}
	slices.SortFunc(releases, func(a, b privateRelease) int {
		if n := a.modified.Compare(b.modified); n != 0 {
			return n
		}
		if a.name < b.name {
			return -1
		}
		if a.name > b.name {
			return 1
		}
		return 0
	})
	cutoff := now.UTC().Add(-s.config.Retention)
	for _, release := range releases {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if release.name == string(s.config.Release) || (!release.modified.Before(cutoff) && total <= s.config.StorageBytes) {
			continue
		}
		if err = s.removeRelease(ctx, root, release); err != nil {
			return result, err
		}
		result.Releases++
		result.Bytes += release.bytes
		total -= release.bytes
	}
	if total > s.config.StorageBytes {
		return result, safeerror.NewFault(fault.Conflict, "active private source maps exceed storage bound")
	}
	return result, ctx.Err()
}

func (s *Store) inspectRelease(ctx context.Context, root *os.Root, name string, budget *int) (out privateRelease, owned bool, err error) {
	out.name = name
	rel, err := openDirectory(root, name)
	if err != nil {
		return out, false, err
	}
	defer rel.Close()
	out.info, err = rel.Stat(".")
	if err != nil {
		return out, false, err
	}
	portals, err := directoryEntries(ctx, rel, maxPortals)
	if err != nil {
		return out, false, err
	}
	*budget -= len(portals)
	if *budget < 0 {
		return out, false, inventoryExceeded()
	}
	for _, entry := range portals {
		if !slices.Contains(s.config.Portals, telemetry.Portal(entry.Name())) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return out, false, nil
		}
		portal, err := openDirectory(rel, entry.Name())
		if err != nil {
			return out, false, err
		}
		inspected, size, modified, owned, err := inspectPortal(ctx, portal, name, entry.Name(), budget)
		portal.Close()
		if err != nil || !owned {
			return out, false, err
		}
		out.portals = append(out.portals, inspected)
		if size > math.MaxInt64-out.bytes {
			return out, false, inventoryExceeded()
		}
		out.bytes += size
		if modified.After(out.modified) {
			out.modified = modified
		}
	}
	return out, len(out.portals) > 0, nil
}

func inspectPortal(ctx context.Context, root *os.Root, release, name string, budget *int) (out privatePortal, size int64, modified time.Time, owned bool, err error) {
	out.name = name
	out.info, err = root.Stat(".")
	if err != nil {
		return
	}
	manifest, readErr := readManifest(ctx, root, release, name)
	if missing(readErr) {
		return
	}
	if readErr != nil {
		err = readErr
		return
	}
	names := map[string]bool{"manifest.json": true}
	for _, file := range manifest.Assets {
		names[file] = true
	}
	entries, readErr := directoryEntries(ctx, root, maxFiles)
	if readErr != nil {
		err = readErr
		return
	}
	*budget -= len(entries)
	if *budget < 0 {
		err = inventoryExceeded()
		return
	}
	if len(entries) != len(names) {
		return
	}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return
		}
		if !names[entry.Name()] {
			return
		}
		info, statErr := root.Lstat(entry.Name())
		if statErr != nil {
			err = statErr
			return
		}
		if !info.Mode().IsRegular() {
			return
		}
		out.files = append(out.files, privateFile{name: entry.Name(), info: info})
		if info.Size() < 0 || info.Size() > math.MaxInt64-size {
			err = inventoryExceeded()
			return
		}
		size += info.Size()
		if info.ModTime().After(modified) {
			modified = info.ModTime()
		}
	}
	owned = true
	return
}

func equalRelease(a, b privateRelease) bool {
	if !sameFile(a.info, b.info) || len(a.portals) != len(b.portals) {
		return false
	}
	portals := make(map[string]privatePortal, len(a.portals))
	for _, portal := range a.portals {
		portals[portal.name] = portal
	}
	for _, portal := range b.portals {
		before, ok := portals[portal.name]
		if !ok || !sameFile(before.info, portal.info) || len(before.files) != len(portal.files) {
			return false
		}
		files := make(map[string]os.FileInfo, len(before.files))
		for _, file := range before.files {
			files[file.name] = file.info
		}
		for _, file := range portal.files {
			if !sameFile(files[file.name], file.info) {
				return false
			}
		}
	}
	return true
}

func (s *Store) removeRelease(ctx context.Context, root *os.Root, before privateRelease) error {
	budget := maxInventory
	after, owned, err := s.inspectRelease(ctx, root, before.name, &budget)
	if err != nil {
		return err
	}
	if !owned || !equalRelease(before, after) {
		return changed()
	}
	rel, err := openDirectory(root, before.name)
	if err != nil {
		return err
	}
	defer rel.Close()
	info, err := rel.Stat(".")
	if err != nil {
		return err
	}
	if !sameFile(before.info, info) {
		return changed()
	}
	for _, portal := range before.portals {
		if err := removePortal(ctx, rel, portal); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Directory metadata changes during our removals; identity must still match.
	linked, err := root.Lstat(before.name)
	if err != nil {
		return err
	}
	if !linked.IsDir() || !os.SameFile(before.info, linked) {
		return changed()
	}
	return root.Remove(before.name)
}

func removePortal(ctx context.Context, rel *os.Root, before privatePortal) error {
	root, err := openDirectory(rel, before.name)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !sameFile(before.info, info) {
		return changed()
	}
	// Delete the manifest last, keeping ownership evidence on partial failure.
	files := slices.Clone(before.files)
	slices.SortFunc(files, func(a, b privateFile) int {
		if a.name == b.name {
			return 0
		}
		if a.name == "manifest.json" {
			return 1
		}
		if b.name == "manifest.json" {
			return -1
		}
		return 0
	})
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := root.Lstat(file.name)
		if err != nil {
			return err
		}
		if !sameFile(file.info, current) {
			return changed()
		}
		if err := root.Remove(file.name); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	linked, err := rel.Lstat(before.name)
	if err != nil {
		return err
	}
	if !linked.IsDir() || !os.SameFile(before.info, linked) {
		return changed()
	}
	return rel.Remove(before.name)
}
