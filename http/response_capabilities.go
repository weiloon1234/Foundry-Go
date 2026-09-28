package http

import (
	"bufio"
	"io"
	"net"
	stdhttp "net/http"
)

// controlledResponseWriter owns pending body bytes. Optional native controls
// must pass through that owner before reaching the underlying transport.
type controlledResponseWriter interface {
	stdhttp.ResponseWriter
	io.ReaderFrom
	FlushError() error
	Unwrap() stdhttp.ResponseWriter
	underlyingWriter() stdhttp.ResponseWriter
	hijackResponse() (net.Conn, *bufio.ReadWriter, error)
	enableFullDuplexResponse() error
}

type responseFlusher struct{ writer controlledResponseWriter }

func (f responseFlusher) Flush() { _ = f.writer.FlushError() }

type responseHijacker struct{ writer controlledResponseWriter }

func (h responseHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.writer.hijackResponse()
}

type responsePusher struct{ writer controlledResponseWriter }

func (p responsePusher) Push(target string, options *stdhttp.PushOptions) error {
	return p.writer.underlyingWriter().(stdhttp.Pusher).Push(target, options)
}

// responseCapabilities advertises exactly the underlying writer's direct
// Flusher/Hijacker/Pusher support. ReaderFrom belongs to the buffering owner,
// so io.Copy cannot bypass body capture, accounting or transformation.
func responseCapabilities(writer controlledResponseWriter) stdhttp.ResponseWriter {
	_, flush := writer.underlyingWriter().(stdhttp.Flusher)
	_, hijack := writer.underlyingWriter().(stdhttp.Hijacker)
	_, push := writer.underlyingWriter().(stdhttp.Pusher)
	f, h, p := responseFlusher{writer}, responseHijacker{writer}, responsePusher{writer}
	switch {
	case flush && hijack && push:
		return struct {
			controlledResponseWriter
			responseFlusher
			responseHijacker
			responsePusher
		}{writer, f, h, p}
	case flush && hijack:
		return struct {
			controlledResponseWriter
			responseFlusher
			responseHijacker
		}{writer, f, h}
	case flush && push:
		return struct {
			controlledResponseWriter
			responseFlusher
			responsePusher
		}{writer, f, p}
	case hijack && push:
		return struct {
			controlledResponseWriter
			responseHijacker
			responsePusher
		}{writer, h, p}
	case flush:
		return struct {
			controlledResponseWriter
			responseFlusher
		}{writer, f}
	case hijack:
		return struct {
			controlledResponseWriter
			responseHijacker
		}{writer, h}
	case push:
		return struct {
			controlledResponseWriter
			responsePusher
		}{writer, p}
	default:
		return writer
	}
}

// A transparent wrapper may hide an optional interface. ResponseController
// still reaches the buffering owner before unwrapping to the native writer.
// Deadlines continue through the native chain; full-duplex activation visits
// the buffering owner first so it cannot delay a native streaming response.
type responseController struct{ controlledResponseWriter }

func (c responseController) Unwrap() stdhttp.ResponseWriter {
	return c.underlyingWriter()
}
func (c responseController) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return c.hijackResponse()
}

func (c responseController) EnableFullDuplex() error {
	return c.enableFullDuplexResponse()
}

func responseFlushSupported(writer stdhttp.ResponseWriter) bool {
	for range 64 {
		// Foundry owners expose FlushError even when their native transport does
		// not. Inspect through known owners before allowing any buffer commit.
		if owner, ok := writer.(controlledResponseWriter); ok {
			writer = owner.underlyingWriter()
			if writer == nil {
				return false
			}
			continue
		}
		if _, ok := writer.(interface{ FlushError() error }); ok {
			return true
		}
		if _, ok := writer.(stdhttp.Flusher); ok {
			return true
		}
		wrapper, ok := writer.(interface{ Unwrap() stdhttp.ResponseWriter })
		if !ok {
			return false
		}
		writer = wrapper.Unwrap()
		if writer == nil {
			return false
		}
	}
	return false
}
