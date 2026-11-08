package http

import (
	"context"
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const assetRootCheckInterval = time.Second

// assetDirectory retains the identity and leases of one opened directory.
// Assets.mu protects these fields, including roots retired by a replacement.
type assetDirectory struct {
	*os.Root
	info      os.FileInfo
	leases    int
	nextCheck time.Time
}

func openAssetDirectory(path string) (*assetDirectory, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil {
		return nil, safeerror.Join("operation failed", err, root.Close())
	}
	return &assetDirectory{Root: root, info: info, nextCheck: time.Now().Add(assetRootCheckInterval)}, nil
}

// refreshDirectory runs under Assets.mu, at most once per interval even when
// the directory is temporarily missing during a build. Existing leases keep
// their root; new acquisitions use the replacement without waiting for bodies.
// Failed checks or opens leave the current root available until the next check.
func (a *Assets) refreshDirectory() {
	previous := a.root
	if previous == nil {
		return
	}
	now := time.Now()
	if now.Before(previous.nextCheck) {
		return
	}
	previous.nextCheck = now.Add(assetRootCheckInterval)
	info, err := os.Stat(a.config.Source.directory)
	if err != nil || os.SameFile(previous.info, info) {
		return
	}
	root, err := openAssetDirectory(a.config.Source.directory)
	if err != nil {
		return
	}
	a.root = root
	a.stats.mu.Lock()
	a.stats.entries = nil
	a.stats.mu.Unlock()
	a.closeRetiredDirectory(previous)
}

func (a *Assets) closeRetiredDirectory(root *assetDirectory) {
	if root != nil && root != a.root && root.leases == 0 {
		if err := root.Close(); a.closeErr == nil {
			a.closeErr = err
		}
	}
}

// AssetsModule prepares a source during Build, opens its directory during Boot
// and closes it after managed kernels/requests exit. Consumers declare the source
// and resolve *Assets while assembling routes; they do not manage os.Root.
func AssetsModule(name foundation.ProviderID, key foundation.Key[*Assets], config AssetsConfig) foundation.Module {
	config = config.snapshot()
	return foundation.Module{Name: name, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, key, func(foundation.Resolver) (*Assets, error) { return prepareAssets(config) })
	}, OnBoot: func(ctx context.Context, runtime *foundation.Runtime) error {
		assets, err := foundation.Resolve(runtime.Services(), key)
		if err != nil {
			return err
		}
		if err := assets.start(ctx); err != nil {
			return err
		}
		if err := runtime.OnShutdown("assets", func(ctx context.Context) error { return assets.Close(ctx) }); err != nil {
			return safeerror.Join("operation failed", err, assets.Close(context.WithoutCancel(ctx)))
		}
		return nil
	}}
}
