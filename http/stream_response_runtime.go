package http

import (
	"io"
	stdhttp "net/http"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func (p *preparedFile) writeStream(w stdhttp.ResponseWriter, r *stdhttp.Request) error {
	var writer *fileResponseWriter
	var transfer error
	owned := callback.Isolated("HTTP stream response", func() error {
		writer = newFileResponseWriter(r.Context(), w)
		header := writer.Header()
		p.headers(header)
		header.Set("Accept-Ranges", "none")
		if length, ok := p.length.Get(); ok {
			header.Set("Content-Length", strconv.FormatInt(length, 10))
		}
		if r.Method == stdhttp.MethodHead {
			writer.WriteHeader(stdhttp.StatusOK)
			return nil
		}
		transfer = p.copyStream(writer)
		return nil
	})
	return p.finish(w, r, writer, owned, transfer)
}

// Hold the final chunk until EOF has been checked. Otherwise a producer that
// exceeds Content-Length could send a complete-looking success before its
// excess is discovered. One fixed buffer and a single lookahead byte suffice.
func (p *preparedFile) copyStream(w *fileResponseWriter) error {
	ceiling := p.limit
	expected, known := p.length.Get()
	if known {
		ceiling = expected
	}
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		remaining := ceiling - total
		n, err := p.reader.Read(buffer[:min(int64(len(buffer)), remaining+1)])
		if err != nil && err != io.EOF {
			return err
		}
		if int64(n) > remaining {
			return fault.New(fault.Internal, "stream exceeds its declared length or byte limit")
		}
		finished := err == io.EOF
		if !finished && int64(n) == remaining {
			// io.Reader may return data and EOF together, or EOF on a subsequent read.
			var probe [1]byte
			for {
				count, next := p.reader.Read(probe[:])
				if next != nil && next != io.EOF {
					return next
				}
				if count != 0 {
					return fault.New(fault.Internal, "stream exceeds its declared length or byte limit")
				}
				if next == io.EOF {
					finished = true
					break
				}
			}
		}
		if finished && known && total+int64(n) != expected {
			return io.ErrUnexpectedEOF
		}
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				return err
			}
			total += int64(n)
		}
		if finished {
			if !w.committed {
				w.WriteHeader(stdhttp.StatusOK)
			}
			return nil
		}
	}
}
