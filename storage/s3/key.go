package s3

import (
	"net/url"

	"github.com/weiloon1234/Foundry-Go/storage"
	"golang.org/x/text/unicode/norm"
)

// R2 normalizes keys at storage time. Reject aliases before provider I/O instead
// of allowing a distinct ObjectKey to address or replace the canonical object.
func validateKeyText(provider Provider, text string) error {
	if provider == R2 && !norm.NFC.IsNormalString(text) {
		return storage.Failure(storage.Unsupported, storage.PutOperation, storage.Unchanged, nil)
	}
	return nil
}

// AWS listing responses encode spaces as + and literal plus signs as %2B.
// R2 keeps the existing path-style decoding contract. Decode exactly once so
// literal percent sequences remain part of the object key.
func (b *Backend) decodeListedKey(encoded string) (string, error) {
	if b.config.Provider == AWS {
		return url.QueryUnescape(encoded)
	}
	return url.PathUnescape(encoded)
}
