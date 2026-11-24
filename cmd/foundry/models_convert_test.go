package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/modelconvert"
)

func conversionFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "go.mod" {
			data = []byte(strings.Replace(string(data), "module github.com/weiloon1234/Foundry-Go", "module foundry.test/conversion", 1) + "\nrequire github.com/weiloon1234/Foundry-Go v0.0.0\nreplace github.com/weiloon1234/Foundry-Go => " + root + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "internal/modelconvert/testdata/plain.before"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "models.go"), data, 0600); err != nil {
		t.Fatal(err)
	}
	testDir := filepath.Join(dir, "testonly")
	if err := os.Mkdir(testDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testDir, "empty_test.go"), []byte("package testonly\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestModelsConvertCommand(t *testing.T) {
	for _, mode := range []string{"convert", "dry-run", "refusal", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			dir := conversionFixture(t)
			source := filepath.Join(dir, "models.go")
			before, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "refusal" {
				before = append(before, []byte(`
type Blocked struct { ID int }
type OtherTable struct { orm.Table[Blocked,int] }
var BlockedModels=orm.Define("blocked",func(b orm.Builder[Blocked],m *Blocked)OtherTable{return OtherTable{orm.TableOf(b,&m.ID)}})
`)...)
				if err := os.WriteFile(source, before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"convert", "--dir", dir}
			if mode == "dry-run" {
				args = append(args, "--dry-run")
			}
			var output bytes.Buffer
			if mode == "mismatch" {
				generator := modelconvert.GeneratorFunc(func(ctx context.Context, dir string) error {
					file := filepath.Join(dir, "models.go")
					data, err := os.ReadFile(file)
					if err != nil {
						return err
					}
					data = bytes.ReplaceAll(data, []byte("table=users"), []byte("table=others"))
					if err := os.WriteFile(file, data, 0600); err != nil {
						return err
					}
					_, err = generate.Generate(ctx, generate.Options{Dir: dir})
					return err
				})
				err = convertModels(t.Context(), args[1:], &output, io.Discard, generator)
			} else {
				err = runModels(t.Context(), args, &output, io.Discard)
			}
			if mode == "refusal" || mode == "mismatch" {
				if err == nil {
					t.Fatalf("expected refusal: %s", output.String())
				}
				want := "rename"
				if mode == "mismatch" {
					want = "declaration changed"
				}
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %s: %s / %v", want, output.String(), err)
				}
			} else if err != nil {
				t.Fatalf("%v\n%s", err, output.String())
			}
			after, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "convert" {
				if !bytes.Equal(before, after) {
					t.Fatal("unpublished source changed")
				}
				if _, err := os.Stat(filepath.Join(dir, "models_foundry.gen.go")); !os.IsNotExist(err) {
					t.Fatal("unpublished generated file exists")
				}
				return
			}
			if !bytes.Contains(after, []byte("//foundry:model")) {
				t.Fatal("model not converted")
			}
			generated, err := os.ReadFile(filepath.Join(dir, "models_foundry.gen.go"))
			if err != nil {
				t.Fatal(err)
			}
			output.Reset()
			if err := runModels(t.Context(), args, &output, io.Discard); err != nil {
				t.Fatal(err)
			}
			again, _ := os.ReadFile(filepath.Join(dir, "models_foundry.gen.go"))
			if !bytes.Equal(generated, again) {
				t.Fatal("second run changed output")
			}
			if !strings.Contains(output.String(), "0 model(s)") {
				t.Fatal(output.String())
			}
		})
	}
}

func TestModelsConvertExtendsOwnedOutput(t *testing.T) {
	dir := conversionFixture(t)
	args := []string{"convert", "--dir", dir}
	var output bytes.Buffer
	if err := runModels(t.Context(), args, &output, io.Discard); err != nil {
		t.Fatalf("%v: %s", err, output.String())
	}
	path := filepath.Join(dir, "models.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.Replace(source, []byte("import ("), []byte("import (\n\"github.com/weiloon1234/Foundry-Go/database/orm\""), 1)
	source = append(source, []byte(`
type Team struct{ID int64}
type TeamTable struct{orm.Table[Team,int64]}
var Teams=orm.Define("teams",func(b orm.Builder[Team],m *Team)TeamTable{return TeamTable{orm.TableOf(b,&m.ID)}})
`)...)
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runModels(t.Context(), args, &output, io.Discard); err != nil {
		t.Fatalf("%v: %s", err, output.String())
	}
	if !strings.Contains(output.String(), ".Team") || !strings.Contains(output.String(), "1 model(s)") {
		t.Fatal(output.String())
	}
	generated, err := os.ReadFile(filepath.Join(dir, "models_foundry.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range []string{"type UserTable struct", "type TeamTable struct"} {
		if !bytes.Contains(generated, []byte(symbol)) {
			t.Fatal("missing", symbol)
		}
	}
}

func TestModelsConvertDryRunReportsAllMismatches(t *testing.T) {
	dir := conversionFixture(t)
	file := filepath.Join(dir, "models.go")
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	second := strings.ReplaceAll(string(source[strings.Index(string(source), "// User is retained."):]), "User", "Account")
	second = strings.ReplaceAll(strings.ReplaceAll(second, "users", "accounts"), "Greeting", "Welcome")
	before := append(source, []byte("\n"+second)...)
	if err := os.WriteFile(file, before, 0600); err != nil {
		t.Fatal(err)
	}
	generator := modelconvert.GeneratorFunc(func(ctx context.Context, dir string) error {
		path := filepath.Join(dir, "models.go")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = bytes.ReplaceAll(data, []byte(`json:"name"`), []byte(`json:"name" foundry:"column=label"`))
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
		_, err = generate.Generate(ctx, generate.Options{Dir: dir})
		return err
	})
	var output bytes.Buffer
	err = convertModels(t.Context(), []string{"--dir", dir, "--dry-run"}, &output, io.Discard, generator)
	if err == nil {
		t.Fatal("mismatch succeeded")
	}
	for _, want := range []string{"models.go:", "model User", "model Account", `Name: name differs: hand-written "name", generated "label"`} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q:\n%s", want, &output)
		}
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused package changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "models_foundry.gen.go")); !os.IsNotExist(err) {
		t.Fatal("dry run published output")
	}
}

func TestModelsConvertGenerationFailureDiagnostic(t *testing.T) {
	for _, mode := range []string{"declaration", "compiler"} {
		t.Run(mode, func(t *testing.T) {
			dir := conversionFixture(t)
			var output bytes.Buffer
			want := "key field Missing does not exist"
			if mode == "compiler" {
				want = "undefined: MissingType"
			}
			generator := modelconvert.GeneratorFunc(func(ctx context.Context, stage string) error {
				path := filepath.Join(stage, "models.go")
				source, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if mode == "compiler" {
					source = append(source, []byte("\nvar broken MissingType\n")...)
				} else {
					source = bytes.Replace(source, []byte("key=ID"), []byte("key=Missing"), 1)
				}
				if err := os.WriteFile(path, source, 0600); err != nil {
					return err
				}
				_, err = generate.Generate(ctx, generate.Options{Dir: stage})
				return err
			})
			err := convertModels(t.Context(), []string{"--dir", dir, "--dry-run"}, &output, io.Discard, generator)
			if err == nil {
				t.Fatal("invalid staged source accepted")
			}
			for _, part := range []string{"models User", "generate staged declarations", want} {
				if !strings.Contains(output.String(), part) {
					t.Fatalf("missing %q: %s", part, output.String())
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "models_foundry.gen.go")); !os.IsNotExist(err) {
				t.Fatal("failed proof published output")
			}
		})
	}
}

func TestModelsConvertReportsMemberDocumentation(t *testing.T) {
	dir := conversionFixture(t)
	file := filepath.Join(dir, "models.go")
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.Replace(source, []byte(" Name string"), []byte(" // Domain documentation.\n Name string"), 1)
	source = bytes.Replace(source, []byte(" Name orm.Text"), []byte(" // Member documentation.\n Name orm.Text"), 1)
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runModels(t.Context(), []string{"convert", "--dry-run", "--dir", dir}, &out, io.Discard); err != nil {
		t.Fatalf("%v: %s", err, &out)
	}
	if !strings.Contains(out.String(), "User.Name: documentation from UserTable.Name appended below existing field documentation") {
		t.Fatal(out.String())
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(after, source) {
		t.Fatal("dry run changed source", err)
	}
}
