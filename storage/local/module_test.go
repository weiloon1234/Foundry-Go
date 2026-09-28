package local_test

import (
	"context"
	"errors"
	"os"
	"testing"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

func TestStorageModulesPrepareWithoutIOAndDrainBeforeRootClose(t *testing.T) {
	root := t.TempDir()
	adapterKey := foundation.NewKey[*local.Backend]("test.storage.local")
	diskKey := foundation.NewKey[*storage.Disk]("test.storage.files")
	adapter := local.Module("test.storage.local", adapterKey, local.DefaultConfig(root))
	module := storage.Module("test.storage.files", diskKey, storage.DefineDisk("files"), storage.DefaultConfig(), []foundation.ProviderID{adapter.Name}, func(r foundation.Resolver) (storage.Backend, error) { return foundation.Resolve(r, adapterKey) })
	app, err := foundry.New().Register(module, adapter).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatal("construction touched root", err)
	}
	disk, err := foundation.Resolve(app.Services(), diskKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	object := key(t, "module")
	if _, err := disk.PutBytes(t.Context(), object, []byte("body"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	reader, _, err := disk.Open(t.Context(), object, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if disk.Stats().Active != 0 || !disk.Stats().Closing {
		t.Fatal("shutdown retained disk work")
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, storage.Closed) {
		t.Fatal("shutdown retained reader", err)
	}
	backend, err := foundation.Resolve(app.Services(), adapterKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Start(t.Context()); !errors.Is(err, storage.Closed) {
		t.Fatal("closed backend restarted", err)
	}
}
