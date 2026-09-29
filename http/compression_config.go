package http

import (
	"compress/gzip"
	"io"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ContentEncoding is a response coding, distinct from header names and media types.
type ContentEncoding string

const (
	GzipEncoding   ContentEncoding = "gzip"
	BrotliEncoding ContentEncoding = "br"
)

// GzipLevel preserves gzip's supported levels, including its special defaults.
type GzipLevel int

const (
	GzipDefault  GzipLevel = gzip.DefaultCompression
	GzipFastest  GzipLevel = gzip.BestSpeed
	GzipSmallest GzipLevel = gzip.BestCompression
)

// BrotliQuality ranges from 0 (fastest) to 11 (smallest).
type BrotliQuality int

// BrotliWindow is the base-2 window size, from 10 to 24.
type BrotliWindow int

// CompressionEncoder is an immutable, validated adapter descriptor. Construct
// it through GzipCompression or BrotliCompression; its zero value is invalid.
type CompressionEncoder struct {
	encoding ContentEncoding
	level    int
	window   int
}

func GzipCompression(level GzipLevel) CompressionEncoder {
	return CompressionEncoder{encoding: GzipEncoding, level: int(level)}
}
func BrotliCompression(quality BrotliQuality, window BrotliWindow) CompressionEncoder {
	return CompressionEncoder{encoding: BrotliEncoding, level: int(quality), window: int(window)}
}
func (e CompressionEncoder) Encoding() ContentEncoding { return e.encoding }
func (e CompressionEncoder) Validate() error {
	switch e.encoding {
	case GzipEncoding:
		if e.level >= gzip.HuffmanOnly && e.level <= gzip.BestCompression && e.window == 0 {
			return nil
		}
	case BrotliEncoding:
		if e.level >= 0 && e.level <= 11 && e.window >= 10 && e.window <= 24 {
			return nil
		}
	}
	return fault.New(fault.Invalid, "invalid compression encoder or resource options")
}

type compressionStream interface {
	io.WriteCloser
	Flush() error
}

// resettableStream is implemented by the gzip and Brotli writers. Reset discards
// all encoder state while retaining allocated windows, hash tables and buffers.
type resettableStream interface {
	compressionStream
	Reset(io.Writer)
}

func (e CompressionEncoder) writer(dst io.Writer) (resettableStream, error) {
	switch e.encoding {
	case GzipEncoding:
		return gzip.NewWriterLevel(dst, e.level)
	case BrotliEncoding:
		return newBrotliStream(dst, e.level, e.window), nil
	default:
		return nil, fault.New(fault.Invalid, "compression encoder is not defined")
	}
}

// compressionPools reuses encoder state for each configured (algorithm,
// level, window) within one assembled middleware; nothing is process-global.
// A pooled stream writes through its own sink, which is detached on release so
// an idle pooled encoder never retains a completed response writer.
type compressionPools struct {
	encoders []CompressionEncoder
	pools    []sync.Pool
}

type pooledCompressionStream struct {
	stream resettableStream
	sink   *compressionSink
}

type compressionSink struct{ dst io.Writer }

func (s *compressionSink) Write(data []byte) (int, error) {
	if s.dst == nil {
		return 0, io.ErrClosedPipe
	}
	return s.dst.Write(data)
}

func newCompressionPools(encoders []CompressionEncoder) *compressionPools {
	return &compressionPools{encoders: encoders, pools: make([]sync.Pool, len(encoders))}
}

// acquire returns a reset stream writing to dst and its release function.
// Callers release exactly once, after the stream is no longer used.
func (p *compressionPools) acquire(encoder CompressionEncoder, dst io.Writer) (compressionStream, func(), error) {
	index := -1
	if p != nil {
		index = slices.Index(p.encoders, encoder)
	}
	if index < 0 {
		stream, err := encoder.writer(dst)
		return stream, func() {}, err
	}
	item, _ := p.pools[index].Get().(*pooledCompressionStream)
	if item == nil {
		sink := &compressionSink{dst: dst}
		stream, err := encoder.writer(sink)
		if err != nil {
			return nil, nil, err
		}
		item = &pooledCompressionStream{stream: stream, sink: sink}
	} else {
		item.sink.dst = dst
		item.stream.Reset(item.sink)
	}
	return item.stream, func() {
		item.sink.dst = nil
		p.pools[index].Put(item)
	}, nil
}

// CompressionConfig chooses server preference order, bounded buffering and the
// maximum concurrently active encoders for each assembled middleware instance.
// The first equal-weight encoding wins. Saturation falls back to identity when
// accepted; when a client refuses identity the request waits briefly for an
// encoder, then returns 503 with Retry-After. Requests never queue unbounded
// encoder work. Encoder state is pooled per configured encoder and reused.
type CompressionConfig struct {
	Encoders      []CompressionEncoder
	MinBytes      int
	MaxConcurrent int
}

func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Encoders:      []CompressionEncoder{BrotliCompression(4, 20), GzipCompression(GzipFastest)},
		MinBytes:      1024,
		MaxConcurrent: 32,
	}
}
func (c CompressionConfig) Validate() error {
	if len(c.Encoders) == 0 || len(c.Encoders) > 2 || c.MinBytes < 0 || c.MinBytes > 64<<10 || c.MaxConcurrent < 1 || c.MaxConcurrent > 1024 {
		return fault.New(fault.Invalid, "compression configuration exceeds its bounds")
	}
	seen := make(map[ContentEncoding]bool)
	for _, e := range c.Encoders {
		if err := e.Validate(); err != nil {
			return err
		}
		if seen[e.encoding] {
			return fault.New(fault.Duplicate, "compression encoding is repeated")
		}
		seen[e.encoding] = true
	}
	return nil
}
func (c CompressionConfig) clone() CompressionConfig {
	c.Encoders = slices.Clone(c.Encoders)
	return c
}
