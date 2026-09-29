package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

type assetStat struct{ mode fs.FileMode }

// assetStatTTL bounds how long a successful lookup is reused. Directory sources
// observe replaced files within this window; misses are never cached, so a new
// file is visible immediately.
const (
	assetStatTTL     = time.Second
	assetStatEntries = 4096
)

type assetStatEntry struct {
	stat    assetStat
	expires time.Time
}

type assetStatCache struct {
	mu      sync.RWMutex
	entries map[string]assetStatEntry
}

func (c *assetStatCache) get(name string, now time.Time) (assetStat, bool) {
	c.mu.RLock()
	entry, ok := c.entries[name]
	c.mu.RUnlock()
	if !ok || !now.Before(entry.expires) {
		return assetStat{}, false
	}
	return entry.stat, true
}

func (c *assetStatCache) put(name string, stat assetStat, now time.Time) {
	c.mu.Lock()
	if c.entries == nil || len(c.entries) >= assetStatEntries {
		c.entries = make(map[string]assetStatEntry)
	}
	c.entries[name] = assetStatEntry{stat: stat, expires: now.Add(assetStatTTL)}
	c.mu.Unlock()
}

// assetTag is a strong content validator for a file without a modification
// time. The recorded size detects a replaced file in a mutable filesystem.
type assetTag struct {
	size int64
	tag  EntityTag
}

func assetSourceError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	result := err
	owned := callback.Isolated("HTTP asset error classification", func() error {
		switch {
		case ctx.Err() != nil && errorgraph.Is(err, ctx.Err()):
			result = Unavailable.WithCause(err)
		case errorgraph.Is(err, fs.ErrNotExist):
			result = NotFound.WithCause(err)
		case errorgraph.Is(err, fs.ErrPermission):
			result = Forbidden.WithCause(err)
		}
		return nil
	})
	if owned != nil {
		return InternalError.WithCause(owned)
	}
	return result
}
func (a *Assets) stat(ctx context.Context, name string) (assetStat, error) {
	release, err := a.acquire(ctx)
	if err != nil {
		return assetStat{}, err
	}
	defer release()
	now := time.Now()
	if cached, ok := a.stats.get(name, now); ok {
		return cached, nil
	}
	var result assetStat
	var returned error
	owned := callback.Isolated("HTTP asset stat", func() error {
		var info fs.FileInfo
		if a.root != nil {
			info, returned = a.root.Stat(name)
		} else {
			info, returned = assetFilesystemStat(a.config.Source.filesystem, name)
		}
		if returned == nil {
			if info == nil {
				returned = fault.New(fault.Internal, "asset source returned no file info")
			} else {
				result.mode = info.Mode()
			}
		}
		return nil
	})
	if owned != nil {
		return result, InternalError.WithCause(owned)
	}
	if returned == nil {
		returned = ctx.Err()
	}
	if returned == nil {
		a.stats.put(name, result, now)
	}
	return result, assetSourceError(ctx, returned)
}
func assetFilesystemStat(source fs.FS, name string) (info fs.FileInfo, err error) {
	if source, ok := source.(fs.StatFS); ok {
		return source.Stat(name)
	}
	file, err := source.Open(name)
	if file != nil {
		defer func() { err = errors.Join(err, closeAssetFile(file)) }()
	}
	if err != nil {
		return nil, err
	}
	if file == nil {
		return nil, fault.New(fault.Internal, "asset filesystem returned no file")
	}
	return file.Stat()
}
func closeAssetFile(file io.Closer) error {
	var returned error
	owned := callback.Isolated("HTTP asset file close", func() error { returned = file.Close(); return nil })
	if owned != nil {
		return owned
	}
	return returned
}

type assetBody struct {
	io.ReadSeekCloser
	release func()
	once    sync.Once
	err     error
}

func (b *assetBody) Close() error {
	b.once.Do(func() { defer b.release(); b.err = closeAssetFile(b.ReadSeekCloser) })
	return b.err
}

// Download creates a deferred, inline file response using the declared asset
// media rules. It opens nothing until consumed by a successful typed endpoint.
func (a *Assets) Download(name AssetPath) Download {
	if a == nil {
		return Download{err: fault.New(fault.Invalid, "assets are required")}
	}
	if err := assetFilePath(name); err != nil {
		return Download{err: err}
	}
	return DownloadFrom(func(ctx context.Context) (DownloadContent, error) { return a.open(ctx, string(name)) }).WithDisposition(DispositionInline).WithMediaType(a.config.media(string(name)))
}
func (a *Assets) open(ctx context.Context, name string) (content DownloadContent, err error) {
	if !a.config.allowed(name) {
		return content, NotFound
	}
	release, err := a.acquire(ctx)
	if err != nil {
		return content, err
	}
	transferred := false
	var body io.ReadSeekCloser
	// The source retains its lease and body on every non-returning callback path.
	defer func() {
		if !transferred {
			if body != nil {
				err = errors.Join(err, closeAssetFile(body))
			}
			release()
		}
	}()
	if a.root != nil {
		content, err = LocalDownload(a.root, name).source(ctx)
		body = content.Body
	} else {
		content, err = openFilesystemAsset(a.config.Source.filesystem, name)
		body = content.Body
		if err == nil && content.Modified.IsZero() {
			content.EntityTag, err = a.contentTag(name, body)
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return DownloadContent{}, assetSourceError(ctx, err)
	}
	if body == nil {
		return DownloadContent{}, fault.New(fault.Internal, "asset source returned no body")
	}
	content.Body = &assetBody{ReadSeekCloser: body, release: release}
	transferred = true
	return content, nil
}

// contentTag returns a cached strong validator for a file that reports no
// modification time (embed.FS entries report none), hashing its contents once.
// Without it such files could never produce a 304. The body is rewound.
func (a *Assets) contentTag(name string, body io.ReadSeeker) (EntityTag, error) {
	size, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	if cached, ok := a.tags.Load(name); ok && cached.(assetTag).size == size {
		_, err := body.Seek(0, io.SeekStart)
		return cached.(assetTag).tag, err
	}
	if size > a.config.Limits.Bytes {
		_, err := body.Seek(0, io.SeekStart)
		return "", err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hash := sha256.New()
	hashed, err := io.Copy(hash, body)
	if err != nil {
		return "", err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if hashed != size {
		return "", nil
	}
	tag := EntityTag("\"" + hex.EncodeToString(hash.Sum(nil)) + "\"")
	a.tags.Store(name, assetTag{size: size, tag: tag})
	return tag, nil
}

func openFilesystemAsset(source fs.FS, name string) (content DownloadContent, err error) {
	file, err := source.Open(name)
	transferred := false
	if file != nil {
		defer func() {
			if !transferred {
				err = errors.Join(err, closeAssetFile(file))
			}
		}()
	}
	if err != nil {
		return content, err
	}
	if file == nil {
		return content, fault.New(fault.Internal, "asset source returned no file")
	}
	info, err := file.Stat()
	if err != nil {
		return content, err
	}
	if info == nil || !info.Mode().IsRegular() {
		return content, NotFound
	}
	body, ok := file.(io.ReadSeekCloser)
	if !ok {
		return content, fault.New(fault.Invalid, "static asset files must support seeking")
	}
	// Evaluate potentially custom FileInfo methods before ownership transfers.
	name, modified := info.Name(), info.ModTime()
	content = DownloadContent{Body: body, Name: name, Modified: modified, MediaType: "application/octet-stream"}
	transferred = true
	return content, nil
}
