package retirement

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReplacementHeadsResolve(t *testing.T) {
	entries, err := retiredDeclarations()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{"database": "database", "value": "value"}
	for owner := range entries {
		owners[path.Base(owner)] = owner
	}
	symbols := make(map[string]map[string]bool)
	heads := regexp.MustCompile(`\b([a-z][a-z0-9]*)\.([A-Z][A-Za-z0-9_]*)`)
	for owner, members := range entries {
		seen := make(map[string]bool)
		for _, member := range members {
			if member.Name == "" || seen[member.Name] {
				t.Fatalf("invalid or duplicate ledger entry %s.%s", owner, member.Name)
			}
			seen[member.Name] = true
			for _, head := range heads.FindAllStringSubmatch(member.Replacement, -1) {
				pkg, known := owners[head[1]]
				if !known {
					t.Errorf("%s.%s: unknown replacement package %s", owner, member.Name, head[1])
					continue
				}
				if symbols[pkg] == nil {
					symbols[pkg] = make(map[string]bool)
					dir := filepath.Join("../..", pkg)
					files, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range files {
						if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
							continue
						}
						file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
						if err != nil {
							t.Fatal(err)
						}
						for _, decl := range file.Decls {
							switch d := decl.(type) {
							case *ast.FuncDecl:
								if d.Recv == nil {
									symbols[pkg][d.Name.Name] = true
								}
							case *ast.GenDecl:
								for _, spec := range d.Specs {
									switch s := spec.(type) {
									case *ast.TypeSpec:
										symbols[pkg][s.Name.Name] = true
									case *ast.ValueSpec:
										for _, name := range s.Names {
											symbols[pkg][name.Name] = true
										}
									}
								}
							}
						}
					}
				}
				if !symbols[pkg][head[2]] {
					t.Errorf("%s.%s: replacement %s does not exist in %s", owner, member.Name, head[0], pkg)
				}
			}
		}
	}
}

func TestRetirementReplacementDiagnostics(t *testing.T) {
	for _, test := range []struct{ code, want string }{
		{"orm.DoesntHave", "orm.DoesntHave is retired; use orm.Not(orm.Has(relation, predicates...))."},
		{"attachments.AddFiles", "attachments.AddFiles is retired; use attachments.AddFilesTo."},
		{"orm.DeclarationSnapshot", "orm.DeclarationSnapshot is retired; no replacement is recorded; remove this use and consult the current task guide."},
		{"orm.Predicate.Not", "orm.Predicate.Not is retired; use orm.Not(predicate)."},
	} {
		err := CheckDocument("guide.md", []byte("# Guide\n\nUse `"+test.code+"`.\n"))
		if err == nil || !strings.Contains(err.Error(), "guide.md:3: "+test.want) {
			t.Fatalf("%s: %v", test.code, err)
		}
	}
}

func TestReplacementLedgerPreservesNameConsumers(t *testing.T) {
	entries, err := retiredDeclarations()
	if err != nil {
		t.Fatal(err)
	}
	names, err := declarations()
	if err != nil {
		t.Fatal(err)
	}
	for owner, members := range entries {
		if len(names[owner]) != len(members) {
			t.Fatalf("lost names for %s", owner)
		}
		for i, member := range members {
			if names[owner][i] != member.Name {
				t.Fatalf("lost %s.%s", owner, member.Name)
			}
		}
	}
}

func TestUnqualifiedRetirementKeepsCodeLine(t *testing.T) {
	err := CheckDocument("guide.md", []byte("# Guide\n\n```go\nvar current = 1\n\nDoesntHave(relation)\n```\n"))
	if err == nil || !strings.Contains(err.Error(), "guide.md:6: orm.DoesntHave is retired; use orm.Not(") {
		t.Fatalf("retirement lost its source line: %v", err)
	}
}

func TestReplacementReportingRespectsExternalOwners(t *testing.T) {
	for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
		for _, alias := range []string{"validation", "v"} {
			for _, owner := range []string{"example.test/validation", framework + "validation"} {
				code := "```go\npackage p\nimport " + alias + " \"" + owner + "\"\nvar check = " + alias + ".Rule.Check\n```\n"
				err := check("AGENTS.md", []byte(code))
				if owner == "example.test/validation" {
					if err != nil {
						t.Fatalf("external owner rejected with alias %s: %v", alias, err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "validation.Rule.Check is retired;") {
					t.Fatalf("framework owner missed with alias %s: %v", alias, err)
				}
			}
		}
	}
}

func TestNestedPackageReplacementAliases(t *testing.T) {
	for _, test := range []struct{ owner, alias, member, replacement string }{
		{"testkit/factory", "f", "New", "factory.Define"},
		{"database/prune", "p", "Model", "prune.ORMModel or prune.ORMSoftModel"},
	} {
		for _, alias := range []string{test.alias, path.Base(test.owner), "."} {
			expression := alias + "." + test.member
			if alias == "." {
				expression = test.member
			}
			code := "```go\npackage p\nimport " + alias + " \"" + framework + test.owner + "\"\nvar use = " + expression + "\n```\n"
			err := CheckApplicationDocument("AGENTS.md", []byte(code))
			if err == nil || !strings.Contains(err.Error(), "AGENTS.md:4: "+path.Base(test.owner)+"."+test.member+" is retired; use "+test.replacement+".") || strings.Contains(err.Error(), "no replacement") {
				t.Fatalf("lost replacement through %s: %v", alias, err)
			}
		}
		err := CheckDocument("guide.md", []byte("Use `"+path.Base(test.owner)+"."+test.member+"`.\n"))
		if err == nil || !strings.Contains(err.Error(), "use "+test.replacement+".") {
			t.Fatalf("lost direct replacement: %v", err)
		}
	}
}
