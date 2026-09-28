package storing_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/storing"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestIndependentConsumerUsesFrameworkStorageLifecycle(t *testing.T) {
	app, err := foundry.New().Register(storing.LocalProviders(t.TempDir())...).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	disk, err := foundation.Resolve(app.Services(), storing.FileDisk)
	if err != nil {
		t.Fatal(err)
	}
	key, err := storage.ParseKey("documents/report.bin")
	if err != nil {
		t.Fatal(err)
	}
	object, err := storing.StoreDocument(t.Context(), disk, key, strings.NewReader("document"))
	if err != nil || object.Disk != storing.Files.ID() {
		t.Fatal(object, err)
	}
	if _, err := storing.StoreDocument(t.Context(), disk, key, strings.NewReader("replacement")); !errors.Is(err, storage.PreconditionFailed) {
		t.Fatal("duplicate document replaced", err)
	}
	data, info, err := disk.ReadBytes(t.Context(), key, 1024, storage.ReadOptions{})
	if err != nil || string(data) != "document" || info.Object.ETag != object.Object.ETag {
		t.Fatal("consumer received different representation", err)
	}
	if err := storing.DownloadDocument(disk, key).Validate(); err != nil {
		t.Fatal(err)
	}
}
