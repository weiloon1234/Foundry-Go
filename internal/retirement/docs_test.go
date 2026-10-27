package retirement

import "testing"

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
