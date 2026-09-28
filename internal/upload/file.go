package upload

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
)

// File is an opaque handle to a fully captured request-owned upload. Its zero
// value is absent. Metadata remains readable after cleanup; Open does not.
type File struct {
	owner                    *Batch
	key                      string
	name, declared, detected string
	size                     int64
}

func (f File) IsZero() bool      { return f.owner == nil }
func (f File) Name() string      { return f.name }
func (f File) Size() int64       { return f.size }
func (f File) Extension() string { return filename.Extension(f.name) }

// ClientContentType is untrusted metadata supplied by the client.
func (f File) ClientContentType() string { return f.declared }

// ContentType is Go's bounded content sniff, not proof of a valid image/document.
func (f File) ContentType() string { return f.detected }
func (File) String() string        { return "uploaded file" }
func (File) GoString() string      { return "uploaded file" }
func (File) MarshalJSON() ([]byte, error) {
	return nil, fault.New(fault.Invalid, "uploaded files require multipart transport")
}

var _ json.Marshaler = File{}

// Open returns a new reader at offset zero. The caller closes it after use;
// request cleanup also closes retained readers. Neither the temporary filename
// nor an unrestricted os.File is exposed. Both contexts govern every operation.
func (f File) Open(ctx context.Context) (io.ReadSeekCloser, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "upload reader requires a context")
	}
	if f.owner == nil {
		return nil, fs.ErrInvalid
	}
	b := f.owner
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ready(ctx); err != nil {
		return nil, err
	}
	if len(b.readers) >= b.config.MaxReaders {
		return nil, &LimitError{Kind: Readers}
	}
	raw, err := b.root.Open(f.key)
	if err != nil {
		return nil, err
	}
	info, err := raw.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != f.size {
		_ = raw.Close()
		if err != nil {
			return nil, err
		}
		return nil, fault.New(fault.Internal, "captured upload changed")
	}
	reader := &Reader{owner: b, raw: raw, ctx: ctx}
	b.readers[reader] = struct{}{}
	return reader, nil
}

type Reader struct {
	owner  *Batch
	raw    *os.File
	ctx    context.Context
	closed bool
}

func (*Reader) String() string { return "uploaded file reader" }
func (r *Reader) begin() error {
	if r == nil || r.owner == nil {
		return fs.ErrClosed
	}
	b := r.owner
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.closed {
		return fs.ErrClosed
	}
	if err := b.ready(r.ctx); err != nil {
		return err
	}
	b.active++
	return nil
}
func (r *Reader) Read(p []byte) (int, error) {
	if err := r.begin(); err != nil {
		return 0, err
	}
	defer r.owner.end()
	n, err := r.raw.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	if canceled := r.owner.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	if err := r.begin(); err != nil {
		return 0, err
	}
	defer r.owner.end()
	at, err := r.raw.Seek(offset, whence)
	if canceled := r.ctx.Err(); canceled != nil {
		return at, canceled
	}
	if canceled := r.owner.ctx.Err(); canceled != nil {
		return at, canceled
	}
	return at, err
}
func (r *Reader) Close() error {
	if r == nil || r.owner == nil {
		return nil
	}
	b := r.owner
	b.mu.Lock()
	defer b.mu.Unlock()
	return r.close()
}
func (r *Reader) close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	delete(r.owner.readers, r)
	return r.raw.Close()
}
