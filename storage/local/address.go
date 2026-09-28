package local

import "github.com/weiloon1234/Foundry-Go/storage"

func (b *Backend) Locate(key storage.ObjectKey) (storage.ObjectAddress, error) {
	if b == nil {
		return storage.ObjectAddress{}, storage.Invalid
	}
	if err := key.Validate(); err != nil {
		return storage.ObjectAddress{}, err
	}
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if !b.started || b.closed {
		return storage.ObjectAddress{}, storage.Closed
	}
	// Copies of a managed store retain its identity. Treat them conservatively as
	// aliases until provisioned as a different store, rather than risk self-moves.
	return storage.NewObjectAddress("local:"+b.storeID, key.String())
}
