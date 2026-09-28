package http

import (
	"io"
	stdhttp "net/http"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type compressionMode uint8

const (
	compressionPending compressionMode = iota
	compressionIdentity
	compressionEncoded
	compressionRejected
)

type compressionResponse struct {
	underlying     stdhttp.ResponseWriter
	request        *stdhttp.Request
	header         stdhttp.Header
	final          stdhttp.Header
	status         int
	mode           compressionMode
	config         CompressionConfig
	preferences    compressionPreferences
	permits        chan struct{}
	acquired       bool
	buffer         []byte
	stream         compressionStream
	err            error
	written        int64
	declaredLength int64
	hijacked       bool
	passthrough    bool
}

func (w *compressionResponse) Header() stdhttp.Header         { return w.header }
func (w *compressionResponse) Unwrap() stdhttp.ResponseWriter { return responseController{w} }
func (w *compressionResponse) WriteHeader(status int) {
	if status < 100 || status > 999 {
		panic("invalid HTTP status code")
	}
	if w.status != 0 || w.hijacked {
		return
	}
	if status < 200 && status != 101 {
		copyResponseHeaders(w.underlying.Header(), w.header)
		w.underlying.WriteHeader(status)
		return
	}
	w.status = status
	w.final = w.header.Clone()
	if text := w.final.Get("Content-Length"); text != "" {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil && n >= 0 {
			w.declaredLength = n
		}
	}
	if bodylessCompressionStatus(status) || status == 101 {
		w.commit(false)
	}
}
func (w *compressionResponse) Write(data []byte) (int, error) {
	if w.hijacked {
		return 0, stdhttp.ErrHijacked
	}
	if w.err != nil {
		return 0, w.err
	}
	if err := w.request.Context().Err(); err != nil {
		w.err = err
		return 0, err
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if w.err != nil {
		return 0, w.err
	}
	if bodylessCompressionStatus(w.status) {
		return 0, stdhttp.ErrBodyNotAllowed
	}
	if w.mode == compressionRejected {
		return len(data), nil
	}
	if w.declaredLength >= 0 && int64(len(data)) > w.declaredLength-w.written {
		w.err = stdhttp.ErrContentLength
		return 0, w.err
	}
	if _, explicit := w.final["Content-Type"]; !explicit && w.final.Get("Content-Encoding") == "" && len(data) > 0 {
		sample := data
		if len(sample) > 512 {
			sample = sample[:512]
		}
		w.final.Set("Content-Type", stdhttp.DetectContentType(sample))
	}
	if w.request.Method == stdhttp.MethodHead {
		w.written += int64(len(data))
		return len(data), nil
	}
	if w.mode == compressionPending {
		_, available := w.preferences.choose(w.config.Encoders)
		allowed := !w.passthrough && compressionAllowed(w.status, w.final)
		if !available || !allowed {
			w.commit(false)
		} else if len(data) >= w.config.MinBytes-len(w.buffer) || w.preferences.quality("identity") == 0 {
			w.commit(true)
		} else {
			if w.buffer == nil {
				w.buffer = make([]byte, 0, w.config.MinBytes)
			}
			w.buffer = append(w.buffer, data...)
			w.written += int64(len(data))
			return len(data), nil
		}
		if w.err != nil {
			return 0, w.err
		}
		if w.mode == compressionRejected {
			return len(data), nil
		}
	}
	n, err := w.writeData(data)
	w.written += int64(n)
	return n, err
}
func (w *compressionResponse) writeData(data []byte) (int, error) {
	var target io.Writer = w.underlying
	if w.stream != nil {
		target = w.stream
	}
	total := 0
	for len(data) != 0 {
		if err := w.request.Context().Err(); err != nil {
			w.err = err
			return total, err
		}
		chunk := data
		if len(chunk) > responseBufferPageBytes {
			chunk = chunk[:responseBufferPageBytes]
		}
		n, err := target.Write(chunk)
		if n < 0 || n > len(chunk) {
			w.err = fault.New(fault.Internal, "HTTP compression writer returned an invalid count")
			return total, w.err
		}
		total += n
		if err == nil && n != len(chunk) {
			err = io.ErrShortWrite
		}
		if err != nil {
			w.err = err
			return total, err
		}
		data = data[n:]
	}
	return total, nil
}
func (w *compressionResponse) commit(compress bool) {
	if w.mode != compressionPending || w.err != nil {
		return
	}
	if err := w.request.Context().Err(); err != nil {
		w.err = err
		return
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if w.status == stdhttp.StatusNotModified {
		w.adjustUnwrittenRepresentation(stdhttp.StatusOK)
	}
	appendVary(w.final, "Accept-Encoding")
	if !bodylessCompressionStatus(w.status) && w.status != 101 {
		if coding := w.final.Get("Content-Encoding"); coding != "" {
			if !w.acceptsExistingCoding() {
				w.reject(NotAcceptable)
				return
			}
		} else if compress {
			encoder, ok := w.preferences.choose(w.config.Encoders)
			if ok {
				select {
				case w.permits <- struct{}{}:
					w.acquired = true
					stream, err := encoder.writer(w.underlying)
					if err != nil {
						w.err = err
						return
					}
					w.stream = stream
					w.mode = compressionEncoded
					w.final.Set("Content-Encoding", string(encoder.encoding))
					w.final.Del("Content-Length")
					invalidateCompressedIntegrity(w.final)
				default:
					if w.preferences.quality("identity") == 0 {
						w.reject(Unavailable)
						return
					}
				}
			}
		}
		if w.mode != compressionEncoded && w.final.Get("Content-Encoding") == "" && w.preferences.quality("identity") == 0 {
			w.reject(NotAcceptable)
			return
		}
	}
	if w.mode != compressionEncoded {
		w.mode = compressionIdentity
	}
	copyResponseHeaders(w.underlying.Header(), w.final)
	w.underlying.WriteHeader(w.status)
	if len(w.buffer) != 0 {
		_, w.err = w.writeData(w.buffer)
		w.buffer = nil
	}
}
func (w *compressionResponse) acceptsExistingCoding() bool {
	lines := w.final.Values("Content-Encoding")
	bytes := 0
	count := 0
	for _, line := range lines {
		bytes += len(line)
		if bytes > 1024 {
			return false
		}
		for _, part := range strings.Split(line, ",") {
			name := strings.ToLower(strings.TrimSpace(part))
			count++
			if count > 8 || name == "identity" || name == "*" || HeaderName(name).Validate() != nil || w.preferences.quality(name) == 0 {
				return false
			}
		}
	}
	return count > 0
}
func (w *compressionResponse) reject(code ErrorCode) {
	w.mode = compressionRejected
	w.buffer = nil
	copyResponseHeaders(w.underlying.Header(), w.final)
	writeRoutingError(w.underlying, w.request, code)
}
func (w *compressionResponse) finish() error {
	if w.hijacked {
		return nil
	}
	if err := w.request.Context().Err(); err != nil {
		return err
	}
	if w.mode == compressionRejected {
		return nil
	}
	if w.err != nil {
		return w.err
	}
	if w.request.Method == stdhttp.MethodHead {
		w.finishHead()
		if w.err != nil {
			return w.err
		}
		w.publishTrailers()
		return nil
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if w.err != nil {
		return w.err
	}
	if !bodylessCompressionStatus(w.status) && w.declaredLength >= 0 && w.written != w.declaredLength {
		return io.ErrUnexpectedEOF
	}
	if w.mode == compressionPending {
		if w.final.Get("Content-Type") == "" && w.final.Get("Content-Encoding") == "" && len(w.buffer) > 0 {
			w.final.Set("Content-Type", stdhttp.DetectContentType(w.buffer))
		}
		compress := !w.passthrough && compressionAllowed(w.status, w.final) && (len(w.buffer) >= w.config.MinBytes || w.preferences.quality("identity") == 0)
		w.commit(compress)
	}
	if w.err != nil {
		return w.err
	}
	if w.mode == compressionRejected {
		return nil
	}
	if w.stream != nil {
		if err := w.request.Context().Err(); err != nil {
			return err
		}
		if err := w.stream.Close(); err != nil {
			w.err = err
			return err
		}
	}
	w.publishTrailers()
	return nil
}
func (w *compressionResponse) release() {
	if w.acquired {
		<-w.permits
		w.acquired = false
	}
}
func (w *compressionResponse) flush() error {
	if err := w.request.Context().Err(); err != nil {
		return err
	}
	if !responseFlushSupported(w.underlying) {
		return stdhttp.ErrNotSupported
	}
	if w.hijacked {
		return stdhttp.ErrHijacked
	}
	if w.err != nil {
		return w.err
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if w.request.Method == stdhttp.MethodHead {
		w.finishHead()
	}
	if w.mode == compressionPending {
		// A forced early flush selects identity below the threshold, unless the
		// client excludes identity and a safe transform is available.
		compress := !w.passthrough && compressionAllowed(w.status, w.final) && (len(w.buffer) >= w.config.MinBytes || w.preferences.quality("identity") == 0)
		w.commit(compress)
	}
	if w.err != nil {
		return w.err
	}
	if w.mode == compressionRejected {
		return nil
	}
	if w.stream != nil {
		if err := w.stream.Flush(); err != nil {
			w.err = err
			return err
		}
	}
	if err := stdhttp.NewResponseController(w.underlying).Flush(); err != nil {
		w.err = err
		return err
	}
	return nil
}

// ReadFrom prevents io.Copy from bypassing transformation. The reader and writer
// remain owned until return; source I/O must supply its own cancellation/deadline.
func (w *compressionResponse) ReadFrom(reader io.Reader) (int64, error) {
	if w.hijacked {
		return 0, stdhttp.ErrHijacked
	}
	if w.err != nil {
		return 0, w.err
	}
	buffer := make([]byte, responseBufferPageBytes)
	var total int64
	emptyReads := 0
	for {
		if err := w.request.Context().Err(); err != nil {
			w.err = err
			return total, err
		}
		n, err := reader.Read(buffer)
		if n < 0 || n > len(buffer) {
			w.err = fault.New(fault.Internal, "HTTP compression source returned an invalid count")
			return total, w.err
		}
		if n > 0 {
			emptyReads = 0
			written, writeErr := w.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			w.err = err
			return total, err
		}
		if n == 0 {
			emptyReads++
			if emptyReads >= 100 {
				w.err = io.ErrNoProgress
				return total, w.err
			}
		}
	}
}
func compressedIntegrityField(name string) bool {
	switch stdhttp.CanonicalHeaderKey(name) {
	case "Content-Md5", "Digest", "Content-Digest", "Repr-Digest":
		return true
	}
	return false
}
func invalidateCompressedIntegrity(header stdhttp.Header) {
	for name := range header {
		plain := strings.TrimPrefix(name, stdhttp.TrailerPrefix)
		if compressedIntegrityField(plain) || plain != name && strings.EqualFold(plain, "Etag") {
			delete(header, name)
		}
	}
	if etag := header.Get("Etag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		if strings.HasPrefix(etag, "\"") && strings.HasSuffix(etag, "\"") {
			header.Set("Etag", "W/"+etag)
		} else {
			header.Del("Etag")
		}
	}
	var trailers []string
	for _, line := range header.Values("Trailer") {
		for _, name := range strings.Split(line, ",") {
			name = strings.TrimSpace(name)
			if strings.EqualFold(name, "Etag") {
				header.Del("Etag")
			}
			if !compressedIntegrityField(name) && !strings.EqualFold(name, "Etag") {
				trailers = append(trailers, name)
			}
		}
	}
	header.Del("Trailer")
	if len(trailers) != 0 {
		header.Set("Trailer", strings.Join(trailers, ", "))
	}
}
func (w *compressionResponse) publishTrailers() {
	current := w.header
	if w.mode == compressionEncoded {
		current = w.header.Clone()
		for name := range current {
			plain := strings.TrimPrefix(name, stdhttp.TrailerPrefix)
			if plain != name && (compressedIntegrityField(plain) || strings.EqualFold(plain, "Etag")) {
				delete(current, name)
			}
		}
	}
	publishResponseTrailers(w.underlying.Header(), w.final, current)
}
