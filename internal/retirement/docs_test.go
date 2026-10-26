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
