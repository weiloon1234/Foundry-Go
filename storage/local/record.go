package local

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const objectMagic = "FNDOBJ01"
const fixedHeaderBytes = 76

func address(key storage.ObjectKey) (directory, name string) {
	sum := sha256.Sum256([]byte(key.String()))
	name = hex.EncodeToString(sum[:])
	return "objects/" + name[:2], name
}
func encodeHeader(info storage.ObjectInfo, tag []byte) []byte {
	key, kind := info.Key.String(), string(info.ContentType)
	data := make([]byte, fixedHeaderBytes+len(key)+len(kind))
	copy(data, objectMagic)
	binary.BigEndian.PutUint16(data[8:10], uint16(len(key)))
	binary.BigEndian.PutUint16(data[10:12], uint16(len(kind)))
	binary.BigEndian.PutUint64(data[12:20], uint64(info.Size))
	binary.BigEndian.PutUint64(data[20:28], uint64(info.Modified.UTC().UnixMicro()))
	copy(data[28:44], tag)
	digest, _ := info.Checksum.Get()
	copy(data[44:76], digest[:])
	copy(data[76:], key)
	copy(data[76+len(key):], kind)
	return data
}
func (b *Backend) readHeader(file *os.File) (storage.ObjectInfo, int64, error) {
	invalid := func(err error) (storage.ObjectInfo, int64, error) {
		return storage.ObjectInfo{}, 0, failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, err)
	}
	stat, err := file.Stat()
	if err != nil {
		return invalid(err)
	}
	if !stat.Mode().IsRegular() {
		return invalid(nil)
	}
	fixed := make([]byte, fixedHeaderBytes)
	if _, err := file.ReadAt(fixed, 0); err != nil {
		return invalid(err)
	}
	if string(fixed[:8]) != objectMagic {
		return invalid(nil)
	}
	keySize, typeSize := int(binary.BigEndian.Uint16(fixed[8:10])), int(binary.BigEndian.Uint16(fixed[10:12]))
	size := binary.BigEndian.Uint64(fixed[12:20])
	micro := int64(binary.BigEndian.Uint64(fixed[20:28]))
	if keySize < 1 || keySize > storage.MaxKeyBytes || typeSize < 1 || typeSize > storage.MaxContentTypeBytes || size > uint64(b.config.MaxObjectBytes) {
		return invalid(nil)
	}
	offset := int64(fixedHeaderBytes + keySize + typeSize)
	if stat.Size() != offset+int64(size) {
		return invalid(nil)
	}
	text := make([]byte, keySize+typeSize)
	if _, err = file.ReadAt(text, fixedHeaderBytes); err != nil {
		return invalid(err)
	}
	key, err := storage.ParseKey(string(text[:keySize]))
	if err != nil {
		return invalid(err)
	}
	modified, err := temporal.NewDateTime(time.UnixMicro(micro))
	if err != nil {
		return invalid(err)
	}
	var checksum storage.SHA256
	copy(checksum[:], fixed[44:76])
	tag := storage.ETag("\"" + hex.EncodeToString(fixed[28:44]) + "\"")
	info := storage.ObjectInfo{Key: key, Size: int64(size), ContentType: storage.MediaType(text[keySize:]), Modified: modified, ETag: tag, Checksum: value.Set(checksum)}
	if err = info.Validate(); err != nil {
		return invalid(err)
	}
	return info, offset, nil
}
func (b *Backend) openRecord(key storage.ObjectKey) (*os.File, storage.ObjectInfo, int64, error) {
	dir, name := address(key)
	file, err := b.root.Open(dir + "/" + name)
	if err != nil {
		return nil, storage.ObjectInfo{}, 0, failure(storage.Unavailable, storage.OpenOperation, storage.NotApplicable, err)
	}
	info, offset, err := b.readHeader(file)
	if err == nil && info.Key != key {
		err = failure(storage.IntegrityFailed, storage.OpenOperation, storage.NotApplicable, nil)
	}
	if err != nil {
		return nil, storage.ObjectInfo{}, 0, errors.Join(err, failureIf(file.Close(), storage.CloseOperation, storage.NotApplicable))
	}
	return file, info, offset, nil
}
