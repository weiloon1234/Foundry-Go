package upload

import (
	"context"
	"io"
)

// Source is the shared structural contract for a captured file that a feature
// can open. The caller owns the file; each operation closes the reader it opens.
// Name and ClientContentType are untrusted display/type hints.
type Source interface {
	Open(context.Context) (io.ReadSeekCloser, error)
	Name() string
	ClientContentType() string
}
