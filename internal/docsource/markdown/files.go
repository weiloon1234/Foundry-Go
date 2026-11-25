package markdown

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Limits apply before parsing and to streams as well as filesystem inputs.
const (
	// MaxDocumentBytes bounds each Markdown document before parsing.
	MaxDocumentBytes = 4 << 20
	// MaxSourceBytes bounds each compiled source input.
	MaxSourceBytes = 8 << 20
	// MaxManifestBytes bounds each compiler-input manifest or stream.
	MaxManifestBytes = 16 << 20
	// MaxRunBytes bounds retained teaching inputs and anchor indexes per run.
	MaxRunBytes = 128 << 20
	// MaxDocuments bounds teaching documents and cached link targets.
	MaxDocuments = 4096
)

// ReadAll permits at most limit bytes and never formats an arbitrary reader error.
func ReadAll(name string, reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, safeerror.Owned(name+":0: cannot read input; restore read access.", err)
	}
	if int64(len(data)) > limit {
		return nil, safeerror.New(fmt.Sprintf("%s:0: input exceeds maximum %d bytes; split or reduce the input.", name, limit))
	}
	return data, nil
}

// OpenRegular refuses aliases and special files before opening. os.Root also
// confines resolution if an ancestor changes between inspection and open.
func OpenRegular(root, name string, flag int) (*os.File, error) {
	if !filepath.IsLocal(name) {
		return nil, safeerror.New(name + ":0: path is outside repository root; use a repository-relative file.")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, safeerror.Owned(name+":0: cannot inspect repository root.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, safeerror.New(name + ":0: repository root is a symlink; use its real directory.")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, safeerror.Owned(name+":0: cannot open repository root.", err)
	}
	defer r.Close()
	current := ""
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i, part := range parts {
		if strings.HasPrefix(part, ".env") {
			return nil, safeerror.New(name + ":0: secret inputs cannot be read as documentation.")
		}
		current = filepath.Join(current, part)
		info, err = r.Lstat(current)
		if err != nil {
			return nil, safeerror.Owned(name+":0: cannot inspect input; restore the file and its required access permissions.", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, safeerror.New(name + ":0: input contains a symlink; replace it with an owned file or directory.")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, safeerror.New(name + ":0: input must be a regular file.")
		}
	}
	f, err := r.OpenFile(name, flag|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, safeerror.Owned(name+":0: cannot open input; restore read access.", err)
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return nil, safeerror.New(name + ":0: input changed or is not a regular file; retry with an owned regular file.")
	}
	return f, nil
}

// ReadFile confines a bounded regular-file read to the supplied root.
func ReadFile(root, name string, limit int64) ([]byte, error) {
	f, err := OpenRegular(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadAll(name, f, limit)
}

// ReadPath is for standalone inputs whose parent is the caller's trust root.
// Repository inputs should use ReadFile with the repository root instead.
func ReadPath(name string, limit int64) ([]byte, error) {
	return ReadFile(filepath.Dir(name), filepath.Base(name), limit)
}
