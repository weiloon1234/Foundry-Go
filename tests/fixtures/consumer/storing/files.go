// Package storing demonstrates public framework consumption, not an app starter.
package storing

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

var Files = storage.DefineDisk("documents")
var FileDisk = foundation.NewKey[*storage.Disk]("domain.documents")
var localBackend = foundation.NewKey[*local.Backend]("infrastructure.local-files")
var cloudBackend = foundation.NewKey[*s3.Backend]("infrastructure.cloud-files")

func LocalProviders(root string) []foundation.Provider {
	adapter := local.Module("infrastructure.local-files", localBackend, local.DefaultConfig(root))
	disk := storage.Module("domain.documents", FileDisk, Files, storage.DefaultConfig(), []foundation.ProviderID{adapter.Name}, func(r foundation.Resolver) (storage.Backend, error) { return foundation.Resolve(r, localBackend) })
	return []foundation.Provider{disk, adapter}
}
func CloudProviders(config s3.Config) []foundation.Provider {
	adapter := s3.Module("infrastructure.cloud-files", cloudBackend, config)
	disk := storage.Module("domain.documents", FileDisk, Files, storage.DefaultConfig(), []foundation.ProviderID{adapter.Name}, func(r foundation.Resolver) (storage.Backend, error) { return foundation.Resolve(r, cloudBackend) })
	return []foundation.Provider{disk, adapter}
}
func StoreDocument(ctx context.Context, disk *storage.Disk, key storage.ObjectKey, contents io.Reader) (storage.StoredObject, error) {
	return disk.Put(ctx, key, contents, storage.PutOptions{ContentType: storage.Binary, Condition: storage.IfAbsent()})
}
func PersistUpload(ctx context.Context, disk *storage.Disk, key storage.ObjectKey, file foundryhttp.UploadedFile) (storage.StoredObject, error) {
	return storagehttp.StoreUpload(ctx, disk, key, file, storage.PutOptions{})
}
func DownloadDocument(disk *storage.Disk, key storage.ObjectKey) foundryhttp.Download {
	return storagehttp.Download(disk, key, storage.ReadOptions{}).WithName("document.bin")
}
