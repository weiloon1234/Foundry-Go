package scaffold_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin"
	"github.com/weiloon1234/Foundry-Go/plugin/assets"
	"github.com/weiloon1234/Foundry-Go/plugin/scaffold"
)

type input struct{ Title string }

func TestTypedScaffoldInspectionDoesNotRenderAndEditsAreProtected(t *testing.T) {
	declaration := plugin.Manifest{ID: "scaffolds", Version: "1.0.0", Framework: "*"}
	var calls atomic.Int32
	template, err := scaffold.New(declaration, "report", func(_ context.Context, value input) ([]assets.File, error) {
		calls.Add(1)
		return []assets.File{{Path: "report/title.txt", Data: []byte(value.Title)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	extension := plugin.Module{Declaration: declaration, OnRegister: func(r *plugin.Registrar) error { return scaffold.Register(r, template) }}
	builder := foundation.NewBuilder().RegisterPlugin(extension)
	if _, err := builder.Inspect(t.Context()); err != nil {
		t.Fatal(err)
	}
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := scaffold.Declarations(app.Services())
	if err != nil || len(metadata) != 1 || calls.Load() != 0 {
		t.Fatal("inspection rendered scaffold", metadata, err)
	}
	resolved, err := scaffold.Resolve[input](app.Services(), declaration.ID, "report")
	if err != nil || resolved != template {
		t.Fatal("typed scaffold resolution", err)
	}
	if _, err := scaffold.Resolve[string](app.Services(), declaration.ID, "report"); err == nil {
		t.Fatal("wrong input type resolved")
	}
	root := t.TempDir()
	if _, err := resolved.Publish(t.Context(), root, input{Title: "Title"}, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report/title.txt"), []byte("user edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolved.Publish(t.Context(), root, input{Title: "New title"}, false); err == nil {
		t.Fatal("scaffold overwrote consumer edits")
	}
}

func TestScaffoldCallbackFailuresNeverPublishPartialOutput(t *testing.T) {
	declaration := plugin.Manifest{ID: "scaffolds", Version: "1.0.0", Framework: "*"}
	for _, scenario := range []string{"panic", "goexit", "escape", "error", "empty", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			template, err := scaffold.New(declaration, "report", func(context.Context, input) ([]assets.File, error) {
				switch scenario {
				case "panic":
					panic("private input")
				case "goexit":
					runtime.Goexit()
				case "escape":
					return []assets.File{{Path: "../escape", Data: []byte("content")}}, nil
				case "error":
					return []assets.File{{Path: "partial.txt", Data: []byte("content")}}, fmt.Errorf("render failed")
				case "empty":
					return nil, nil
				case "cancel":
					cancel()
				}
				return []assets.File{{Path: "partial.txt", Data: []byte("content")}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if _, err := template.Publish(ctx, root, input{}, false); err == nil || strings.Contains(err.Error(), "private input") {
				t.Fatal("scaffold callback escaped containment", err)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatal("failed render wrote files", entries, err)
			}
		})
	}
}
