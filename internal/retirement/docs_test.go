package retirement

import (
	"path"
	"strings"
	"testing"
)

func TestApplicationDocumentationNames(t *testing.T) {
	for _, text := range []string{
		"Codecs: `Text`, `DateTime`, `Decimal`, `Count`.",
		"Components: `Button`, `Input`, `Select`, `Dialog`, `Table`.",
		"Components: `<Select>`, `<Select value={value}>`, `</Select>`.",
		"```go\nCount(items)\nSelect(options)\nvar callback = Load\n```",
		"Use `Query[S, R].All` and `Query[S, R].OrderBy`.",
		"Use `LegacyWidget` for the application's older widget.",
	} {
		if err := CheckApplicationDocument("AGENTS.md", []byte(text)); err != nil {
			t.Errorf("application names rejected in %q: %v", text, err)
		}
	}
	// Names a current package exports (Count, Select, Load) are accepted bare;
	// retired names nothing exports are not.
	for _, text := range []string{
		"Helpers: `Preflight`, `ScalarQuery`, `LoadMissing`.",
		"Helpers: `DoesntHave`, `Preflight`.",
		"```go\nLoadMissing(items)\nDoesntHave(options)\n```",
	} {
		if err := CheckDocument("docs/guides/example.md", []byte(text)); err == nil {
			t.Errorf("framework accepted bare retired names in %q", text)
		}
	}
}

func TestFrameworkDocumentationPlaceholders(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"database/orm/README.md", "`Mutate<Field>(Input) (Stored, error)` and `Access<Field>() (Result, error)` use"},
		{"database/orm/README.md", "A nil constructor transform selects the matching `Mutate<Field>` method."},
		{"docs/guides/model-declarations.md", "Every Go-mutated field requires an `Input*` carrier. Startup rejects orphan/invalid `Mutate<Field>` and `Access<Field>` methods."},
		{"docs/guides/model-mutators.md", "A nil transform argument to `Input*Of` selects the matching `Mutate<Field>` method. Supply a typed `func(Input) (Stored, error)` instead when the transform is explicit."},
		{"docs/guides/model-mutators.md", "`Mutate<Field>` names a persisted Go field, independently of its SQL column tag."},
		{"docs/guides/orm-migration.md", "Keep `Mutate<Field>` and `Access<Field>` methods as ordinary Go methods. Pass `nil`"},
		{"README.md", "Use `Mutate<Field>` with `<Select>` and `<Select prop>` and `</Select>`."},
		{"README.md", "```\nMutate<Field>(Input) (Stored, error)\nAccess<Field>() (Result, error)\nInput*Of\n```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckDocument(tc.name, []byte(tc.text)); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := CheckDocument("README.md", []byte("Use `Mutate<Field>` with `Mutate`.")); err == nil {
		t.Fatal("placeholder exempted a separate bare retired name")
	}
}

func TestDocumentationJavaScriptAndJSX(t *testing.T) {
	for mode, check := range map[string]func(string, []byte) error{
		"framework":   CheckDocument,
		"application": CheckApplicationDocument,
	} {
		t.Run(mode, func(t *testing.T) {
			for _, text := range []string{
				"Use `<Select>`, `<Select prop>`, `</Select>`, and `<Select />`.",
				"Use `<Select><Select prop /></Select>`.",
				"Use <Select> and <Select prop=\"rows.Count\"> and </Select>.",
				"Use `<rows.Count>` and `<Select prop={endpoint.WithScopes}>`.",
				"```\n<Select prop>\n</Select>\n```",
			} {
				if err := check("AGENTS.md", []byte(text)); err != nil {
					t.Errorf("JSX rejected in %q: %v", text, err)
				}
			}
			for _, language := range []string{"js", "javascript", "jsx", "ts", "typescript", "tsx", "TSX title=components"} {
				for _, marker := range []string{"```", "~~~~"} {
					text := marker + language + "\nrows.Count(items)\nexpr.Select(options)\nendpoint.WithScopes(scopes)\nnullableJoinedText(joined)\n<Select />\n" + marker
					if err := check("AGENTS.md", []byte(text)); err != nil {
						t.Errorf("%s fence rejected: %v", language, err)
					}
					if err := check("AGENTS.md", []byte(text+"\nUse `rows.Count(items)`.")); err == nil {
						t.Errorf("%s fence exempted following Go reference", language)
					}
				}
			}
			for _, text := range []string{
				"Use `<Select>` with `rows.Count(items)`.",
				"Use `<Select>` with `validation.Rule[string].Check(ctx, input, limits)`.",
				"```go\nif count < limit { rows.Count(items) }\n```",
				"```go\nif value <count && rows.Count(items) > 0 {}\n```",
			} {
				if err := check("AGENTS.md", []byte(text)); err == nil {
					t.Errorf("accepted retired Go reference in %q", text)
				}
			}
		})
	}
}

func TestDocumentationQualifiedRetirementLedger(t *testing.T) {
	denied, err := declarations()
	if err != nil {
		t.Fatal(err)
	}
	for owner, members := range denied {
		for _, member := range members {
			spelling := path.Base(owner) + "." + member
			t.Run(spelling, func(t *testing.T) {
				for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
					for _, text := range []string{"Use " + spelling + ".", "Use `" + spelling + "`.", "```go\n" + spelling + "()\n```"} {
						if err := check("AGENTS.md", []byte(text)); err == nil || !strings.Contains(err.Error(), "retired") {
							t.Errorf("accepted %q: %v", text, err)
						}
					}
				}
			})
		}
	}
}

func TestApplicationDocumentationRetiredTeaching(t *testing.T) {
	for _, text := range []string{
		"Use `database/query`.",
		"Use `query.ModelQueryOf[User]()`.",
		"Use `rows.Query[S, R].All` and `rows.Query[S, R].OrderBy`.",
		"Use `translations.Text.Values().Entries()`.",
		"```go\nvalidation.DefineField[Request](\"name\", selectName).Rules(rule)\n```",
	} {
		if err := CheckApplicationDocument("AGENTS.md", []byte(text)); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestApplicationDocumentationStarterRetiredTeaching(t *testing.T) {
	for _, tc := range []struct{ pattern, code string }{
		{".WithBodyValidation(", "endpoint.WithBodyValidation(rule)"},
		{".WithScopes(", "endpoint.WithScopes(scopes)"},
		{".WithPermissions(", "endpoint.WithPermissions(permissions)"},
		{".WithAuthorization(", "endpoint.WithAuthorization(policy)"},
		{"f.Field.Rules", "f.Field.Rules(rules...)"},
		{"rule.Check(", "rule.Check(ctx, input, limits)"},
		{"Values().Entries()", "text.Values().Entries()"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			for _, text := range []string{"Use `" + tc.code + "`.", "```go\n" + tc.code + "\n```"} {
				err := CheckApplicationDocument(".claude/skills/api/SKILL.md", []byte(text))
				if err == nil || !strings.Contains(err.Error(), "ambiguous API teaching") {
					t.Fatalf("accepted former Starter teaching %q: %v", text, err)
				}
			}
		})
	}
}

func TestDocumentationApplicationHelper(t *testing.T) {
	for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
		for _, text := range []string{
			"Use nullableJoinedText for joins.",
			"Use `nullableJoinedText` for joins.",
			"Use `nullableJoinedText(joined)` for joins.",
			"```go\nnullableJoinedText(joined)\n```",
		} {
			if err := check("AGENTS.md", []byte(text)); err != nil {
				t.Fatalf("application helper rejected: %v", err)
			}
		}
	}
}

func TestHistoricalLabelsCannotExemptRetiredTeaching(t *testing.T) {
	for _, name := range []string{"docs/guides/orm-migration.md", "docs/guides/validation.md", "AGENTS.md", ".claude/skills/model/SKILL.md"} {
		for _, body := range []string{"```go\nfield.Rules(rules...)\n```", "Use `r.Check`.", "| Former |\n| --- |\n| `field.Rules(rules...)` |"} {
			if err := CheckDocument(name, []byte("<!-- orm-example: example-before -->\n"+body)); err == nil {
				t.Fatalf("%s accepted retired teaching %q", name, body)
			}
		}
	}
}

func TestRetiredDocumentationBoundaries(t *testing.T) {
	before := "<!-- orm-example: query-before -->\n```go\nquery.ModelQueryOf[User]()\n```\n"
	if err := CheckDocument("docs/guides/orm-migration.md", []byte(before)); err == nil {
		t.Fatal("historical label exempted retired teaching")
	}
	for _, tc := range []struct{ name, text string }{
		{"docs/guides/other.md", before},
		{"docs/guides/orm-migration.md", before + "query.ModelQueryOf[User]()"},
		{"docs/guides/orm-migration.md", "```go\nquery.ModelQueryOf[User]()\n```"},
		{"README.md", "Use orm.Extension(slot,loader)."},
		{"README.md", "Use factory.New(build)."},
	} {
		if err := CheckDocument(tc.name, []byte(tc.text)); err == nil {
			t.Errorf("accepted %s: %s", tc.name, tc.text)
		}
	}
}

func TestRetiredDocumentationUnqualifiedCode(t *testing.T) {
	for _, text := range []string{
		"Use `LoadMissing` and `Preflight`.",
		"Use `Query[S, R].OrderBy`.",
		"Use `Query[S, R].All`.",
		"Use `rows.Query[S, R].All`.",
		"```go\nvar saved = Query[S, R].OrderBy\n```",
		"```go\nvar saved = Query[S, R].All\n```",
		"```\nLoadMissing(ctx, db, parents)\n```",
		"```\nQuery[S, R].OrderBy\n```",
		"~~~\nQuery[S, R].All\n~~~",
		"Use `Query[S, map[K]R].All`.",
		"Use ``Preflight(q)``.",
		"Use `Preflight(ctx, q)`.",
		"Use `ScalarQuery(ctx, db, q, yield)`.",
		"```go\nitems, err := LoadMissing(ctx, db, parents)\n```",
		"~~~go\nvar saved = LoadMissing\n~~~",
		"```go\nreturn LoadMissing\n```",
		"```go\nconsume(LoadMissing)\n```",
		"```go\nvar callback func() = ScalarQuery\n```",
		"```go\nfunc build() { _ = DoesntHave(build) }\n```",
	} {
		if err := CheckDocument("README.md", []byte(text)); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
	if err := CheckDocument("database/orm/rows/README.md", []byte("Use `Query.OrderBy`.")); err == nil {
		t.Fatal("accepted local retired member")
	}
	for _, text := range []string{
		"Load resources. Each example is independent. Compile and Preflight happen before execution.",
		"```go\ntype Message struct { Count int }\nx := Message{Count: 1}\n_ = x\nvalue := orm.Value[Model, V]{}\n_ = value\n```",
		"```go\nq.\nSelect(mapping)\n```",
		"Use `orm.Query[S].OrderBy` and `orm.Query[S].All`.",
		"Use `orm.Compile(q)`, `rows.Read(q).Paginate(ctx, db, request)` and `orm.Query.OrderBy`.",
		"```go\nfmt.Println(\"Load Each Compile\") // Load resources\nrows.EachRead(ctx, db, rows.Read(q), yield)\n```",
		// Declaring a function or method that shares a retired name is not a use of it.
		"```go\nfunc Preflight(dependencies datatable.Dependencies) (*datatable.Manager, error) {\n\treturn datatable.New(dependencies, config)\n}\n```",
		"```go\nfunc (r Reports) LoadMissing(ctx context.Context) error { return nil }\n```",
		"```go\nfunc (r *Reports[T]) ScalarQuery(yield func(T) bool) {}\n```",
	} {
		if err := CheckDocument("README.md", []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	before := "<!-- orm-example: query-before -->\n```go\nLoadMissing(ctx, db, parents)\n```"
	if err := CheckDocument("docs/guides/orm-migration.md", []byte(before)); err == nil {
		t.Fatal("historical label exempted retired teaching")
	}
	if err := CheckDocument("README.md", []byte(before)); err == nil {
		t.Fatal("accepted before marker outside migration guide")
	}
}

func TestRestoredModelDirectiveAndRetiredProjection(t *testing.T) {
	for _, name := range []string{"docs/guides/model-declarations.md", "database/AGENTS.md", "database/orm/README.md"} {
		if err := CheckDocument(name, []byte("//foundry:model table=users var=Users key=ID")); err != nil {
			t.Fatal(err)
		}
		if err := CheckDocument(name, []byte("//foundry:projection")); err == nil {
			t.Fatalf("%s accepted retired projection directive", name)
		}
	}
}

func TestDocumentationRetiredCommands(t *testing.T) {
	denied, err := declarations()
	if err != nil {
		t.Fatal(err)
	}
	rules, err := loadDocumentRules(denied)
	if err != nil {
		t.Fatal(err)
	}
	for command, replacement := range rules.RetiredCommands {
		for _, spelling := range []string{command, strings.TrimPrefix(command, "foundry "), strings.ReplaceAll(command, " ", "\t")} {
			for _, text := range []string{"Run `" + spelling + " ./...`.", "```sh\n" + spelling + " ./...\n```", "Run " + spelling + "."} {
				for _, check := range []func(string, []byte) error{CheckDocument, CheckApplicationDocument} {
					err := check("AGENTS.md", []byte(text))
					if err == nil || !strings.Contains(err.Error(), replacement) {
						t.Fatalf("missing retired command remedy: %v", err)
					}
				}
			}
		}
	}
}

// One rule for every entry, decided from the recorded inventory: a bare retired
// name is refused in a framework document only when no current top-level export
// of any importable package has that name; a qualified retired name always is.
func TestBareRetiredNameFollowsTheInventory(t *testing.T) {
	refused := func(text string) error {
		return CheckDocument("guide.md", []byte("# Guide\n\n"+text+"\n"))
	}
	// Live namesakes: database.JSONContains, database.JSONNull, orm.Load, orm.Count.
	for _, live := range []string{"JSONContains", "JSONNull", "Load", "Count", "Paginate"} {
		if err := refused("The `" + live + "` helper takes a document."); err != nil {
			t.Errorf("bare %s has a current top-level export and was refused: %v", live, err)
		}
		if err := refused("```go\nvar current = " + live + "\n```"); err != nil {
			t.Errorf("bare %s in a code block has a current top-level export and was refused: %v", live, err)
		}
	}
	// No live namesake: refused, and the message lists the retired members it matches.
	err := refused("The `KeyColumnNe` filter.")
	if err == nil || !strings.Contains(err.Error(), "`KeyColumnNe` matches the retired `orm.KeyColumnNe`") {
		t.Fatalf("a bare retired name nothing exports must be refused with the honest message: %v", err)
	}
	// A method or field elsewhere with the same name is not a live namesake.
	if err := refused("The `TimeBetween` filter."); err == nil {
		t.Fatal("TimeBetween is only a method elsewhere; the bare name must stay refused")
	}
	// The qualified spelling of a retired entry is always refused, live namesake or not.
	for qualified, use := range map[string]string{"orm.JSONContains": "orm.ContainsDocument", "orm.Load": ""} {
		err := refused("```go\nvar old = " + qualified + "\n```")
		if err == nil || !strings.Contains(err.Error(), qualified+" is retired") || use != "" && !strings.Contains(err.Error(), "use "+use) {
			t.Errorf("the retired %s must be refused: %v", qualified, err)
		}
	}
}

// The rule reads the inventory, so a name that left the live surface is refused
// bare as soon as the inventory is regenerated; no entry carries a flag.
func TestLiveNamesComeFromTopLevelExportsOnly(t *testing.T) {
	live, err := liveTopLevelNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"JSONNull", "JSONContains", "Load"} {
		if !live[name] {
			t.Errorf("%s should be a current top-level export", name)
		}
	}
	for _, name := range []string{"KeyColumnNe", "TimeBetween", "Foundry.Anything"} {
		if live[name] {
			t.Errorf("%s must not be a live top-level name", name)
		}
	}
}
