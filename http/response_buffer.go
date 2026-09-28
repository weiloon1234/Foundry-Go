package http

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

const responseBufferPageBytes = 32 << 10

// responseBuffer owns bounded pages, avoiding whole-body reallocations or a
// second contiguous copy before conditional transfer. Its owner serializes
// appends and reads; it is not a concurrent response writer.
type responseBuffer struct {
	limit int64
	size  int64
	pages [][]byte
}

// append is atomic with respect to its byte ceiling. A false result leaves all
// captured bytes intact so a caller can flush that prefix and then this input.
func (b *responseBuffer) append(data []byte) bool {
	if b.limit < b.size || int64(len(data)) > b.limit-b.size {
		return false
	}
	for len(data) != 0 {
		if len(b.pages) == 0 || len(b.pages[len(b.pages)-1]) == cap(b.pages[len(b.pages)-1]) {
			capacity := min(int64(responseBufferPageBytes), b.limit-b.size)
			b.pages = append(b.pages, make([]byte, 0, int(capacity)))
		}
		last := len(b.pages) - 1
		count := min(len(data), cap(b.pages[last])-len(b.pages[last]))
		b.pages[last] = append(b.pages[last], data[:count]...)
		b.size += int64(count)
		data = data[count:]
	}
	return true
}

func (b *responseBuffer) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, os.ErrInvalid
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if offset >= b.size {
		return 0, io.EOF
	}
	available := min(int64(len(dst)), b.size-offset)
	written := 0
	for int64(written) < available {
		page := offset / responseBufferPageBytes
		start := int(offset % responseBufferPageBytes)
		count := min(int(available)-written, len(b.pages[page])-start)
		copy(dst[written:written+count], b.pages[page][start:start+count])
		written += count
		offset += int64(count)
	}
	if written < len(dst) {
		return written, io.EOF
	}
	return written, nil
}

func (b *responseBuffer) reader() *io.SectionReader {
	return io.NewSectionReader(b, 0, b.size)
}

func (b *responseBuffer) entityTag() EntityTag {
	hash := sha256.New()
	for _, page := range b.pages {
		_, _ = hash.Write(page)
	}
	return EntityTag("\"" + hex.EncodeToString(hash.Sum(nil)) + "\"")
}

func (b *responseBuffer) reset() {
	b.pages = nil
	b.size = 0
}
