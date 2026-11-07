package developeroperations

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	"github.com/weiloon1234/Foundry-Go/telemetry/sourcemap"
)

// doc-example:begin private-source-maps
func SourceMaps(privateDirectory string, activeRelease telemetry.Release, source clock.Clock) (*sourcemap.Store, error) {
	return sourcemap.New(sourcemap.Config{
		Directory: privateDirectory, Release: activeRelease,
		Portals: []telemetry.Portal{"admin"}, Clock: source,
		MaxBytes: 10 << 20, Retention: 30 * 24 * time.Hour, StorageBytes: 1 << 30,
	})
}

func ReadSourceMap(ctx context.Context, store *sourcemap.Store, release telemetry.Release, path string) (sourcemap.Map, error) {
	asset, err := sourcemap.ParseAsset(path)
	if err != nil {
		return sourcemap.Map{}, err
	}
	return store.Read(ctx, release, asset)
}

func PruneSourceMaps(ctx context.Context, store *sourcemap.Store) (sourcemap.PruneResult, error) {
	return store.Prune(ctx)
}

// doc-example:end private-source-maps
