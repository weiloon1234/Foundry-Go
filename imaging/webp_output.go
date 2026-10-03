package imaging

import (
	"encoding/binary"
	"io"
)

// vpx v0.2.1 writes the chunk's first letter as odd-length RIFF padding.
// Canonicalize only those padding bytes at the trusted encoder boundary,
// retaining strict container validation for untrusted inputs. This streaming
// writer uses constant space and handles arbitrary Write boundaries.
type webpOutput struct {
	out        io.Writer
	header     [12]byte
	used, size int
	remaining  uint64
	padding    bool
	err        error
}

func newWebPOutput(w io.Writer) *webpOutput {
	return &webpOutput{out: w, size: 12}
}

func (w *webpOutput) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for len(data) > 0 {
		n := 1
		part := data[:1]
		switch {
		case w.used < w.size:
			n = min(len(data), w.size-w.used)
			part = data[:n]
			copy(w.header[w.used:], part)
			w.used += n
			if w.used == w.size {
				if w.size == 12 {
					w.size, w.used = 8, 0
				} else {
					w.remaining = uint64(binary.LittleEndian.Uint32(w.header[4:8]))
					w.padding = w.remaining&1 != 0
					if w.remaining == 0 {
						w.used = 0
					}
				}
			}
		case w.remaining > 0:
			n = int(min(uint64(len(data)), w.remaining))
			part = data[:n]
			w.remaining -= uint64(n)
			if w.remaining == 0 && !w.padding {
				w.used = 0
			}
		default:
			part = []byte{0}
			w.padding, w.used = false, 0
		}
		count, err := w.out.Write(part)
		if count < 0 || count > n {
			count, err = 0, io.ErrShortWrite
		}
		written += count
		if err == nil && count != n {
			err = io.ErrShortWrite
		}
		if err != nil {
			w.err = err
			return written, err
		}
		data = data[n:]
	}
	return written, nil
}
