package s3

import (
	"net/url"

	"github.com/weiloon1234/Foundry-Go/storage"
	"golang.org/x/text/unicode/norm"
)

// R2 (and compatible providers declaring it) normalize keys at storage time.
// Reject aliases before provider I/O instead of allowing a distinct ObjectKey
// to address or replace the canonical object.
func validateKeyText(nfc bool, text string) error {
	if nfc && !norm.NFC.IsNormalString(text) {
		return storage.Failure(storage.Unsupported, storage.PutOperation, storage.Unchanged, nil)
	}
	return nil
}
func (c Config) requiresNFC() bool {
	return c.Provider == R2 || c.Provider == Compatible && c.Compatible.RequiresNFCKeys
}

// AWS listing responses encode spaces as + and literal plus signs as %2B.
// R2 keeps the existing path-style decoding contract; compatible providers
// follow AWS unless they declare path encoding. Decode exactly once so literal
// percent sequences remain part of the object key.
func (b *Backend) decodeListedKey(encoded string) (string, error) {
	if b.config.Provider == AWS || b.config.Provider == Compatible && !b.config.Compatible.ListingPathEncoding {
		return url.QueryUnescape(encoded)
	}
	return url.PathUnescape(encoded)
}
