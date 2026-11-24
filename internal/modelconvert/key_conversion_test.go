package modelconvert

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

func TestProofExplicitKeyCodecs(t *testing.T) {
	for _, tc := range []struct{ name, typ, factory string }{
		{"string", "string", "codec.String[string]()"},
		{"interval", "temporal.Interval", "codec.Interval()"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/database/orm";"github.com/weiloon1234/Foundry-Go/database/codec";"github.com/weiloon1234/Foundry-Go/temporal")
var _ temporal.Interval
type Record struct{ ID `+tc.typ+`; Name string }
type RecordTable struct{orm.Table[Record,`+tc.typ+`]; ID orm.Key[Record,`+tc.typ+`]; Name orm.Text[Record,string]}
var Records=orm.Define("records",func(b orm.Builder[Record],m *Record)RecordTable{table:=orm.TableOf(b,&m.ID,orm.Codec(keyCodec()));return RecordTable{table,table.Key(),orm.TextOf(b,&m.Name)}})
func keyCodec()codec.Codec[`+tc.typ+`]{return `+tc.factory+`}
`)
			p := analyze(t, dir)
			if len(p.Refused) > 0 {
				t.Fatal(p.Refused)
			}
			_, err := Prove(t.Context(), p, ProofOptions{FrameworkDirectory: frameworkRoot(t), Generator: GeneratorFunc(func(ctx context.Context, dir string) error {
				_, err := generate.Generate(ctx, generate.Options{Dir: dir})
				return err
			})})
			if err != nil {
				t.Fatalf("proof: %T: %+v", err, err)
			}
		})
	}
}

func TestConvertInputKey(t *testing.T) {
	dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/database/orm";"github.com/weiloon1234/Foundry-Go/model")
type Record struct{ID model.ID[Record]; Name string}
type FreshID struct{Value model.ID[Record]}
func(Record)MutateID(v FreshID)(model.ID[Record],error){return v.Value,nil}
type RecordTable struct{orm.Table[Record,model.ID[Record]];ID orm.InputKey[Record,FreshID,model.ID[Record]];Name orm.Text[Record,string]}
var Records=orm.Define("records",func(b orm.Builder[Record],m *Record)RecordTable{table:=orm.TableOf(b,&m.ID);return RecordTable{table,orm.InputKeyOf[Record,FreshID](b,table.Key(),nil),orm.TextOf(b,&m.Name)}})
`)
	p := analyze(t, dir)
	if len(p.Refused) > 0 {
		t.Fatal(p.Refused)
	}
	if !strings.Contains(string(p.Files[0].After), "key-role=input") || !strings.Contains(string(p.Files[0].After), "input=FreshID") {
		t.Fatal(string(p.Files[0].After))
	}
	_, err := Prove(t.Context(), p, ProofOptions{FrameworkDirectory: frameworkRoot(t), Generator: GeneratorFunc(func(ctx context.Context, dir string) error {
		_, err := generate.Generate(ctx, generate.Options{Dir: dir})
		return err
	})})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProofWorkspaceFailureDiagnostic(t *testing.T) {
	dir := fixture(t, string(read(t, "plain.before")))
	p := analyze(t, dir)
	_, err := Prove(t.Context(), p, ProofOptions{FrameworkDirectory: frameworkRoot(t), ScratchDirectory: filepath.Join(t.TempDir(), "missing", "directory"), Generator: GeneratorFunc(func(context.Context, string) error { t.Fatal("generator reached"); return nil })})
	if err == nil {
		t.Fatal("missing workspace accepted")
	}
	for _, part := range []string{"models User", "create proof workspace", "no such file or directory"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("missing %q: %v", part, err)
		}
	}
}

func TestProofEmbeddedAssetsFailureDiagnostic(t *testing.T) {
	dir := fixture(t, string(read(t, "plain.before")))
	p := analyze(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/sample\nunknown-directive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Prove(t.Context(), p, ProofOptions{FrameworkDirectory: frameworkRoot(t), Generator: GeneratorFunc(func(context.Context, string) error { t.Fatal("generator reached"); return nil })})
	if err == nil {
		t.Fatal("invalid module accepted")
	}
	for _, part := range []string{"models User", "list proof embedded assets", "unknown directive: unknown-directive"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("missing %q: %v", part, err)
		}
	}
}
