package markdown

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
)

// Limits apply before parsing and to streams as well as filesystem inputs.
const (
	// MaxDocumentBytes bounds every Markdown document a documentation command
	// reads or parses. The repository's largest current guide is under 140 KB.
	MaxDocumentBytes = 512 << 10
	// MaxRecordBytes bounds the dated historical records (docs/evidence and the
	// changelog). The repository's largest record is 1.2 MB; see DocumentLimit.
	MaxRecordBytes = 3 << 19
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
		return nil, diagnostic.DocFile.At(name, 0, 0).Say("input", "cannot be read").To("restore read access").Caused(err)
	}
	if int64(len(data)) > limit {
		return nil, diagnostic.DocLimit.At(name, 0, 0).Say("input", fmt.Sprintf("over %d bytes", limit)).To("split or reduce the input").Err()
	}
	return data, nil
}

// OpenRegular refuses aliases and special files before opening. os.Root also
// confines resolution if an ancestor changes between inspection and open.
func OpenRegular(root, name string, flag int) (*os.File, error) {
	if !filepath.IsLocal(name) {
		return nil, diagnostic.DocFile.At(name, 0, 0).Say("path", "outside the repository root").To("use a repository-relative file").Err()
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, diagnostic.DocFile.At(name, 0, 0).Say("repository root", "cannot be inspected").To("restore access to the root directory").Caused(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, diagnostic.DocFile.At(name, 0, 0).Say("repository root", "a symlink").To("use its real directory").Err()
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, diagnostic.DocFile.At(name, 0, 0).Say("repository root", "cannot be opened").To("restore access to the root directory").Caused(err)
	}
	defer r.Close()
	current := ""
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i, part := range parts {
		if strings.HasPrefix(strings.ToLower(part), ".env") {
			return nil, diagnostic.DocFile.At(name, 0, 0).Say("path", "names a secret input").To("keep secrets out of documentation inputs").Err()
		}
		current = filepath.Join(current, part)
		info, err = r.Lstat(current)
		if err != nil {
			return nil, diagnostic.DocFile.At(name, 0, 0).Say("input", "cannot be inspected").To("restore the file and its access permissions").Caused(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, diagnostic.DocFile.At(name, 0, 0).Say("input", "contains a symlink").To("replace it with an owned file or directory").Err()
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, diagnostic.DocFile.At(name, 0, 0).Say("input", "not a regular file").To("use a regular file").Err()
		}
	}
	f, err := r.OpenFile(name, flag|syscall.O_NONBLOCK, 0)
	if err != nil {
		if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
			return nil, diagnostic.DocFile.At(name, 0, 0).Say("document", "cannot be opened for writing").To("make it writable").Caused(err)
		}
		return nil, diagnostic.DocFile.At(name, 0, 0).Say("input", "cannot be opened").To("restore read access").Caused(err)
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return nil, diagnostic.DocFile.At(name, 0, 0).Say("input", "changed while it was opened").To("retry with an owned regular file").Err()
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
