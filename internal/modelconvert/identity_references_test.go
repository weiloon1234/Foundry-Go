package modelconvert

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnreferencedIdentityCanUseConventionalName(t *testing.T) {
	source := strings.ReplaceAll(string(read(t, "plain.before")), "usersIdentity", "oldIdentity")
	plan := analyze(t, fixture(t, source))
	if len(plan.Refused) != 0 || len(plan.Models) != 1 {
		t.Fatal(plan.Refused)
	}
	if strings.Contains(string(plan.Files[0].After), "oldIdentity") {
		t.Fatalf("identity name leaked: %s", plan.Files[0].After)
	}
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "renamed", true: "storage changed"}[changed], func(t *testing.T) {
			generator := GeneratorFunc(func(_ context.Context, stage string) error {
				data := strings.ReplaceAll(string(read(t, "plain.generated")), "usersIdentity", "usersIdentity")
				if changed {
					data = strings.Replace(data, "usersIdentity.ReferenceOf(u)", `model.NewReference[User]("different_storage", u.ID, nil)`, 1)
				}
				return os.WriteFile(filepath.Join(stage, "models_foundry.gen.go"), []byte(data), 0600)
			})
			_, err := Prove(context.Background(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t), ScratchDirectory: t.TempDir()})
			if changed {
				if err == nil || !strings.Contains(err.Error(), "declaration changed: identity") {
					t.Fatalf("identity storage difference not caught: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIdentityExternalReferences(t *testing.T) {
	for _, tc := range []struct {
		name, file, source string
		exported           bool
	}{
		{name: "handwritten", file: "reference.go", source: "package models\nvar storageName=oldIdentity.ModelName()\n"},
		{name: "test", file: "reference_test.go", source: "package models\nvar storageName=oldIdentity.ModelName()\n"},
		{name: "other package", file: "caller/reference.go", source: "package caller\nimport models \"foundry.test/conversion\"\nvar storageName=models.OldIdentity.ModelName()\n", exported: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "oldIdentity"
			if tc.exported {
				name = "OldIdentity"
			}
			dir := fixture(t, strings.ReplaceAll(string(read(t, "plain.before")), "usersIdentity", name))
			path := filepath.Join(dir, tc.file)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			write(t, path, []byte(tc.source))
			plan := analyze(t, dir)
			if len(plan.Refused) != 1 || !strings.Contains(plan.Refused[0].Reason, "referenced outside its removed identity block at "+path+":") {
				t.Fatalf("reference not named: %+v", plan.Refused)
			}
			if len(plan.Files) != 0 {
				t.Fatal("refused identity staged source")
			}
		})
	}
}

func TestIdentityShadowIsNotReference(t *testing.T) {
	source := string(read(t, "plain.before")) + "\nfunc unrelated()string{usersIdentity:=\"local\";return usersIdentity}\n"
	plan := analyze(t, fixture(t, source))
	if len(plan.Refused) > 0 {
		t.Fatal(plan.Refused)
	}
}

func TestConventionalIdentityReferencesRemain(t *testing.T) {
	source := string(read(t, "plain.before")) + "\nvar storageName=usersIdentity.ModelName()\n"
	plan := analyze(t, fixture(t, source))
	if len(plan.Refused) != 0 {
		t.Fatal(plan.Refused)
	}
	if !strings.Contains(string(plan.Files[0].After), "storageName = usersIdentity.ModelName()") {
		t.Fatal("reference changed")
	}
}
