package retirement

import "testing"

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
