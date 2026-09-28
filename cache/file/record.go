package file

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cacheatomic"
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
)

const recordMagic = "FNDCACH1"
const recordMetadataBytes = 8 + 2 + 8 + 8 + 4 + 1
const recordHeaderBytes = recordMetadataBytes + sha256.Size

// Records separate fixed metadata from raw payload. Integrity can be checked
// with a bounded copy buffer, without materializing values for Exists/Expire.
type fileRecord struct {
	file    *os.File
	expires time.Time
	size    int64
	digest  []byte
	hash    hash.Hash
}

func (b *Backend) openRecord(root *os.Root, name, key string) (*fileRecord, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, safe(err)
	}
	if !info.Mode().IsRegular() {
		return nil, fault.New(fault.Invalid, "invalid file cache record")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, safe(err)
	}
	fail := func() (*fileRecord, error) {
		_ = f.Close()
		return nil, fault.New(fault.Invalid, "corrupt file cache record")
	}
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < recordHeaderBytes || info.Size() > int64(b.config.MaxValueBytes)+16384+recordHeaderBytes {
		return fail()
	}
	header := make([]byte, recordHeaderBytes)
	if _, err = io.ReadFull(f, header); err != nil {
		return fail()
	}
	if string(header[:8]) != recordMagic || header[30] > 1 {
		return fail()
	}
	keyBytes := int(binary.BigEndian.Uint16(header[8:10]))
	dataBytes := binary.BigEndian.Uint64(header[10:18])
	nanos := binary.BigEndian.Uint32(header[26:30])
	if keyBytes < 1 || keyBytes > 16384 || dataBytes > uint64(b.config.MaxValueBytes) || info.Size() != int64(recordHeaderBytes+keyBytes)+int64(dataBytes) || nanos >= 1000000000 {
		return fail()
	}
	address := make([]byte, keyBytes)
	if _, err = io.ReadFull(f, address); err != nil {
		return fail()
	}
	hash := sha256.Sum256(address)
	if hex.EncodeToString(hash[:])+".cache" != name || key != "" && key != string(address) {
		return fail()
	}
	r := &fileRecord{file: f, size: int64(dataBytes), digest: header[recordMetadataBytes:], hash: sha256.New()}
	if header[30] == 1 {
		r.expires = time.Unix(int64(binary.BigEndian.Uint64(header[18:26])), int64(nanos)).UTC()
	} else if binary.BigEndian.Uint64(header[18:26]) != 0 || nanos != 0 {
		return fail()
	}
	_, _ = r.hash.Write(header[:recordMetadataBytes])
	_, _ = r.hash.Write(address)
	return r, nil
}
func (r *fileRecord) verify(ctx context.Context, destination io.Writer) error {
	if _, err := copyRecord(ctx, io.MultiWriter(r.hash, destination), r.file, r.size); err != nil {
		return err
	}
	if !bytes.Equal(r.hash.Sum(nil), r.digest) {
		return fault.New(fault.Invalid, "corrupt file cache record")
	}
	return nil
}
func (b *Backend) read(ctx context.Context, root *os.Root, name, key string) (*cacheatomic.Record, error) {
	r, err := b.openRecord(root, name, key)
	if err != nil || r == nil {
		return nil, err
	}
	defer r.file.Close()
	var data bytes.Buffer
	if err = r.verify(ctx, &data); err != nil {
		return nil, err
	}
	return &cacheatomic.Record{Data: data.Bytes(), Expires: r.expires}, nil
}
func (b *Backend) publish(ctx context.Context, root *os.Root, name, key string, expires time.Time, size int64, data io.Reader) error {
	if len(key) > 16384 || size < 0 || size > int64(b.config.MaxValueBytes) {
		return fault.New(fault.Invalid, "invalid cache record size")
	}
	header := make([]byte, recordHeaderBytes)
	copy(header, recordMagic)
	binary.BigEndian.PutUint16(header[8:10], uint16(len(key)))
	binary.BigEndian.PutUint64(header[10:18], uint64(size))
	if !expires.IsZero() {
		binary.BigEndian.PutUint64(header[18:26], uint64(expires.Unix()))
		binary.BigEndian.PutUint32(header[26:30], uint32(expires.Nanosecond()))
		header[30] = 1
	}
	checksum := sha256.New()
	_, _ = checksum.Write(header[:recordMetadataBytes])
	_, _ = checksum.Write([]byte(key))
	temporary := ".pending-" + rand.Text()
	f, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return safe(err)
	}
	defer root.Remove(temporary)
	_, err = f.Write(header)
	if err == nil {
		_, err = f.WriteString(key)
	}
	if err == nil {
		_, err = copyRecord(ctx, io.MultiWriter(f, checksum), data, size)
	}
	if err == nil {
		_, err = f.WriteAt(checksum.Sum(nil), recordMetadataBytes)
	}
	if err == nil && b.config.Sync {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return safe(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = root.Rename(temporary, name); err != nil {
		return safe(err)
	}
	if b.config.Sync {
		return syncRoot(root)
	}
	return nil
}
func copyRecord(ctx context.Context, dst io.Writer, src io.Reader, size int64) (int64, error) {
	buffer := make([]byte, 32<<10)
	limited := io.LimitReader(src, size)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := limited.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if err == io.EOF {
			if total != size {
				return total, io.ErrUnexpectedEOF
			}
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// Inspect validates the envelope, payload size and checksum without decoding or
// buffering payloads. Expire streams a new atomic record under the same lock.
func (b *Backend) Inspect(ctx context.Context, key cache.EntryKey, inspect cacheatomic.Inspection) (bool, error) {
	root, leave, err := b.enter(ctx)
	if err != nil {
		return false, err
	}
	defer leave()
	release, err := filelock.Acquire(ctx, root, lockName)
	if err != nil {
		return false, safe(err)
	}
	defer release()
	name := filename(key)
	r, err := b.openRecord(root, name, key.String())
	if err != nil || r == nil {
		return false, err
	}
	defer r.file.Close()
	if err = r.verify(ctx, io.Discard); err != nil {
		return false, err
	}
	expires, live, err := inspect(r.expires)
	if err != nil || !live || expires == nil {
		return live && err == nil, err
	}
	if _, err = r.file.Seek(int64(recordHeaderBytes+len(key.String())), io.SeekStart); err != nil {
		return false, safe(err)
	}
	if err = b.publish(ctx, root, name, key.String(), *expires, r.size, r.file); err != nil {
		return false, err
	}
	return true, nil
}

func syncRoot(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return safe(err)
	}
	return safe(errors.Join(dir.Sync(), dir.Close()))
}
