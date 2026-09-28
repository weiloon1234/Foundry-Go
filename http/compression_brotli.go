package http

import (
	"io"

	"github.com/andybalholm/brotli"
)

// Use the stable writer API, with an explicit window. Experimental WriterV2 is
// intentionally outside this adapter's contract.
func newBrotliStream(dst io.Writer, quality, window int) compressionStream {
	return brotli.NewWriterOptions(dst, brotli.WriterOptions{Quality: quality, LGWin: window})
}
