package modelconvert

import (
	"bytes"
	"context"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelationsGoldenAndProof(t *testing.T) {
	plan := analyze(t, fixture(t, string(read(t, "relations.before"))))
	if len(plan.Refused) > 0 || len(plan.Models) != 1 {
		t.Fatal(plan.Refused)
	}
	expected, err := format.Source(read(t, "relations.after"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plan.Files[0].After, expected) {
		t.Fatalf("got:\n%s\nwant:\n%s", plan.Files[0].After, expected)
	}
	for _, change := range []bool{false, true} {
		t.Run(map[bool]string{false: "equal", true: "relation rebound"}[change], func(t *testing.T) {
			generator := GeneratorFunc(func(_ context.Context, stage string) error {
				original := string(read(t, "relations.before"))
				start := strings.Index(original, "type ParentTable")
				end := strings.Index(original, "func (p Parent)")
				data := "package models\nimport \"" + ormPath + "\"\n" + original[start:end]
				if change {
					// Rebind the two target-compatible slots through different carrier kinds.
					data = strings.ReplaceAll(data, "Child orm.One[Parent,Child]", "Child orm.Many[Parent,Child]")
					data = strings.ReplaceAll(data, "Children orm.Many[Parent,Child]", "Children orm.One[Parent,Child]")
					data = strings.Replace(data, "orm.OneOf(b,&p.Child),orm.ManyOf(b,&p.Children)", "orm.ManyOf(b,&p.Children),orm.OneOf(b,&p.Child)", 1)
				}
				return os.WriteFile(filepath.Join(stage, "models_foundry.gen.go"), []byte(data), 0600)
			})
			_, err := Prove(context.Background(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t), ScratchDirectory: t.TempDir()})
			if change {
				if err == nil || !strings.Contains(err.Error(), "table type contract changed") {
					t.Fatalf("relation change passed: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
