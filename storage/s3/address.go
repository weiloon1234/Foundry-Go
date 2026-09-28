package s3

import (
	"net/url"
	"strings"

	"github.com/weiloon1234/Foundry-Go/storage"
)

func (b *Backend) Locate(key storage.ObjectKey) (storage.ObjectAddress, error) {
	if b == nil || !b.prepared {
		return storage.ObjectAddress{}, storage.Invalid
	}
	full, err := b.object(key)
	if err != nil {
		return storage.ObjectAddress{}, err
	}
	// AWS general-purpose bucket names identify stores globally; ignoring a
	// custom endpoint is conservative for alternate endpoints of the same bucket.
	store := "aws-s3:" + b.config.Bucket
	if b.config.Provider == R2 {
		endpoint, err := url.Parse(b.config.Endpoint)
		if err != nil {
			return storage.ObjectAddress{}, storage.Invalid
		}
		account, _, _ := strings.Cut(strings.ToLower(endpoint.Hostname()), ".")
		store = "r2:" + account + ":" + b.config.Bucket
	}
	return storage.NewObjectAddress(store, full)
}
