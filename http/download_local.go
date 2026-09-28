package http

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// LocalDownload opens a regular file relative to an application-owned os.Root.
// The caller owns the root's lifecycle; Foundry owns each opened file. Paths use
// fs.ValidPath's relative slash-separated form and never come from display names.
// The root confines symlink resolution; a valid-looking path alone does not.
func LocalDownload(root *os.Root, path string) Download {
	if root == nil || !fs.ValidPath(path) || path == "." {
		return Download{err: fault.New(fault.Invalid, "local download requires a root and relative file path")}
	}
	return DownloadFrom(func(ctx context.Context) (DownloadContent, error) {
		if ctx == nil {
			return DownloadContent{}, fault.New(fault.Invalid, "local download requires a context")
		}
		if err := ctx.Err(); err != nil {
			return DownloadContent{}, err
		}
		// Reject known non-files before opening. On Unix the open itself is
		// nonblocking, so a concurrent swap to a FIFO cannot stall before Stat.
		before, err := root.Stat(path)
		if err != nil {
			return DownloadContent{}, localDownloadError(err)
		}
		if !before.Mode().IsRegular() {
			return DownloadContent{}, fault.New(fault.Invalid, "local download requires a regular file")
		}
		file, err := root.OpenFile(path, downloadOpenFlags, 0)
		if err != nil {
			return DownloadContent{}, localDownloadError(err)
		}
		// Returning the body even when Stat fails preserves ownership for the
		// response preparation boundary. Never abandon an opened resource here.
		content := DownloadContent{Body: file}
		info, err := file.Stat()
		if err != nil {
			return content, err
		}
		if !info.Mode().IsRegular() {
			return content, fault.New(fault.Invalid, "local download requires a regular file")
		}
		content.Name, content.Modified = info.Name(), info.ModTime()
		content.MediaType = MediaType("application/octet-stream")
		if err := ctx.Err(); err != nil {
			return content, err
		}
		return content, nil
	})
}

func localDownloadError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return NotFound.WithCause(err)
	}
	return err
}
