package sourcemap_test

import (
	"context"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/telemetry"
	"github.com/weiloon1234/Foundry-Go/telemetry/sourcemap"
)

func ExampleNew() {
	store, err := sourcemap.New(sourcemap.Config{
		Directory:    "", // Disabled until the application supplies its private build root.
		Release:      telemetry.Release("release-1"),
		Portals:      []telemetry.Portal{"admin"},
		MaxBytes:     10 << 20,
		Retention:    30 * 24 * time.Hour,
		StorageBytes: 1 << 30,
		Clock:        clock.System{},
	})
	if err != nil {
		panic(err)
	}
	result, err := store.Prune(context.Background())
	fmt.Println(result.Releases, result.Bytes, err)
	// Output: 0 0 <nil>
}

func ExampleParseAsset() {
	asset, err := sourcemap.ParseAsset("admin/_assets/app-abc.js")
	fmt.Println(asset, err)
	// Output: admin/_assets/app-abc.js <nil>
}
