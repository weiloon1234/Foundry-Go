package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPScaffoldsRejectDefaultDirectoryInFrameworkSubpackage(t *testing.T) {
	for _, destination := range []string{"", "."} {
		for _, options := range []ScaffoldOptions{
			{Kind: EndpointScaffold, Name: "CreateNote", ID: "notes.create", Method: "POST", Path: "/notes"},
			{Kind: DTOScaffold, Name: "CreateNoteRequest", HTTP: "body"},
			{Kind: DTOScaffold, Name: "NoteResource", HTTP: "response", Role: "response"},
		} {
			t.Run(destination+"/"+options.Name, func(t *testing.T) {
				root := t.TempDir()
				write(t, root, "go.mod", "module "+framework+"\n")
				dir := filepath.Join(root, "transport")
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
				source := "package transport\n"
				write(t, dir, "source.go", source)
				t.Chdir(dir)
				options.Dir = destination
				path, err := Scaffold(t.Context(), options)
				if path != "" || err == nil || !strings.Contains(err.Error(), "application module") {
					t.Fatal(path, err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 1 || entries[0].Name() != "source.go" {
					t.Fatalf("rejected scaffold wrote files: %v %v", entries, err)
				}
				data, err := os.ReadFile(filepath.Join(dir, "source.go"))
				if err != nil || string(data) != source {
					t.Fatal("existing source changed", err)
				}
			})
		}
	}
}

func TestScaffoldDestinationPaths(t *testing.T) {
	for _, layout := range []string{"relative subpackage", "relative module root", "absolute"} {
		for _, options := range []ScaffoldOptions{
			{Kind: EndpointScaffold, Name: "CreateNote", ID: "notes.create", Method: "POST", Path: "/notes"},
			{Kind: DTOScaffold, Name: "CreateNoteRequest", HTTP: "body"},
			{Kind: DTOScaffold, Name: "NoteResource", HTTP: "response", Role: "response"},
			{Kind: CommandScaffold, Name: "Inspect", ID: "app.inspect"},
		} {
			t.Run(layout+"/"+options.Name, func(t *testing.T) {
				// Create fixture module inputs before changing the working directory.
				root := fixture(t, "package sample\n")
				target := root
				switch layout {
				case "relative subpackage":
					target = filepath.Join(root, "transport")
					if err := os.Mkdir(target, 0755); err != nil {
						t.Fatal(err)
					}
					write(t, target, "source.go", "package sample\n")
					t.Chdir(root)
					options.Dir = "transport/../transport"
				case "relative module root":
					t.Chdir(filepath.Dir(root))
					options.Dir = "./" + filepath.Base(root) + "/."
				case "absolute":
					options.Dir = root
				}
				path, err := scaffoldFixture(t.Context(), options)
				if err != nil {
					t.Fatal(err)
				}
				if !filepath.IsAbs(path) || filepath.Dir(path) != target {
					t.Fatalf("destination = %s; want %s", path, target)
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
				if options.Kind == EndpointScaffold {
					for _, name := range []string{"create_note_request_dto.go", "create_note_response_dto.go"} {
						if _, err := os.Stat(filepath.Join(target, name)); err != nil {
							t.Fatal("contract escaped destination", err)
						}
					}
				}
			})
		}
	}
}
