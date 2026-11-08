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
	for _, text := range []string{
		"Codecs: `Text`, `DateTime`, `Decimal`, `Count`.",
		"Components: `Button`, `Input`, `Select`, `Dialog`, `Table`.",
		"```go\nCount(items)\nSelect(options)\n```",
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

func TestMigrationBeforeBlocks(t *testing.T) {
	const label = "<!-- orm-example: validation-before -->\n"
	blocks := []string{
		"```go\nfield.Rules(rules...)\n```\n",
		"| Former method | Function |\n| --- | --- |\n| `field.Rules(rules...)` | `validation.Rules(field, rules...)` |\n",
		"Former method values such as `r.Check`\nrequire a closure during migration.\n",
		"- Capture former method values such as `r.Check`\n  in a closure during migration.\n",
	}
	for _, block := range blocks {
		for _, name := range []string{"docs/guides/orm-migration.md", "docs/guides/v0.2-migration.md"} {
			if err := CheckDocument(name, []byte(label+block)); err != nil {
				t.Errorf("%s: labeled block rejected: %v", name, err)
			}
			if err := CheckDocument(name, []byte(block)); err == nil {
				t.Errorf("%s: accepted unlabeled block %q", name, block)
			}
			for _, next := range []string{"\nUse `r.Check`.\n", "## Current API\nUse `r.Check`.\n", "<!-- current example -->\nUse `r.Check`.\n", "- Use `r.Check`.\n", "---\nUse `r.Check`.\n", "```go\nr.Check(ctx, input, limits)\n```\n"} {
				if err := CheckDocument(name, []byte(label+block+next)); err == nil {
					t.Errorf("%s: label leaked into %q after %q", name, next, block)
				}
			}
		}
		for _, name := range []string{"docs/guides/validation.md", "AGENTS.md", ".claude/skills/model/SKILL.md"} {
			if err := CheckDocument(name, []byte(label+block)); err == nil {
				t.Errorf("%s: accepted migration exception", name)
			}
		}
	}
	for _, text := range []string{label + "\nUse `r.Check`.\n", label + "## Current API\nUse `r.Check`.\n", label + "---\nUse `r.Check`.\n"} {
		if err := CheckDocument("docs/guides/v0.2-migration.md", []byte(text)); err == nil {
			t.Errorf("accepted nonadjacent label: %q", text)
		}
	}
	if err := CheckDocument("AGENTS.md", []byte(label+"Use current APIs.\n")); err == nil {
		t.Fatal("accepted a migration label in app instructions")
	}
}

func TestRetiredDocumentationBoundaries(t *testing.T) {
	before := "<!-- orm-example: query-before -->\n```go\nquery.ModelQueryOf[User]()\n```\n"
	if err := CheckDocument("docs/guides/orm-migration.md", []byte(before)); err != nil {
		t.Fatal(err)
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
		"Use `Load` and `LoadMissing`.",
		"Use `Query[S, R].OrderBy`.",
		"Use `Query[S, R].All`.",
		"Use `rows.Query[S, R].All`.",
		"```go\nvar saved = Query[S, R].OrderBy\n```",
		"```go\nvar saved = Query[S, R].All\n```",
		"```\nLoad(ctx, db, parents)\n```",
		"```\nQuery[S, R].OrderBy\n```",
		"~~~\nQuery[S, R].All\n~~~",
		"Use `Query[S, map[K]R].All`.",
		"Use ``Compile(q)``.",
		"Use `Preflight(ctx, q)`.",
		"Use `Each(ctx, db, q, yield)`.",
		"```go\nitems, err := Load(ctx, db, parents)\n```",
		"~~~go\nvar saved = LoadMissing\n~~~",
		"```go\nreturn Load\n```",
		"```go\nconsume(Load)\n```",
		"```go\nvar callback func() = Each\n```",
		"```go\nfunc build() { _ = New(build) }\n```",
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
		"```go\nfunc New(dependencies datatable.Dependencies) (*datatable.Manager, error) {\n\treturn datatable.New(dependencies, config)\n}\n```",
		"```go\nfunc (r Reports) Load(ctx context.Context) error { return nil }\n```",
		"```go\nfunc (r *Reports[T]) Each(yield func(T) bool) {}\n```",
	} {
		if err := CheckDocument("README.md", []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	before := "<!-- orm-example: query-before -->\n```go\nLoad(ctx, db, parents)\n```"
	if err := CheckDocument("docs/guides/orm-migration.md", []byte(before)); err != nil {
		t.Fatal(err)
	}
	if err := CheckDocument("README.md", []byte(before)); err == nil {
		t.Fatal("accepted before marker outside migration guide")
	}
}
