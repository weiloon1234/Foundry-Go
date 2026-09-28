package assets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin"
	"github.com/weiloon1234/Foundry-Go/plugin/assets"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestAssetsSnapshotRegisterInspectAndPublish(t *testing.T) {
	declaration := plugin.Manifest{ID: "assets", Version: "1.0.0", Framework: "*"}
	source := []byte("original")
	bundle, err := assets.New(declaration, "public", assets.File{Path: "nested/file.txt", Data: source})
	if err != nil {
		t.Fatal(err)
	}
	source[0] = 'X'
	extension := plugin.Module{Declaration: declaration, OnRegister: func(r *plugin.Registrar) error { return assets.Register(r, bundle) }}
	app := testkit.Plugins(t, extension)
	bundles, err := assets.Bundles(app.Services())
	if err != nil || len(bundles) != 1 || bundles[0].Info() != bundle.Info() {
		t.Fatal("bundle inspection", bundles, err)
	}
	files := bundle.Files()
	files[0].Path = "corrupt"
	if bundle.Files()[0].Path != "nested/file.txt" {
		t.Fatal("mutable file metadata")
	}
	root := t.TempDir()
	if _, err := bundle.Publish(t.Context(), root, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "nested/file.txt")); err != nil || string(data) != "original" {
		t.Fatal("bundle retained mutable input", string(data), err)
	}
	wrong := extension
	wrong.Declaration.ID = "other"
	if _, err := foundation.NewBuilder().RegisterPlugin(wrong).Build(t.Context()); err == nil {
		t.Fatal("foreign bundle owner accepted")
	}
	if _, err := assets.New(declaration, "public", assets.File{Path: "duplicate"}, assets.File{Path: "duplicate"}); err == nil {
		t.Fatal("duplicate file path accepted")
	}
}
