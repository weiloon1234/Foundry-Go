package invalid

import (
	"context"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
)

func invalid(disk *storage.Disk, key storage.ObjectKey) func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
	return func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		return storagehttp.Download(disk, key, storage.ReadOptions{}), nil
	}
}
