package main

import (
	"strings"
	"testing"
)

func TestMigratedQueryGuidePolicy(t *testing.T) {
	const link = "\n[Migration](orm-migration.md)\n"
	for _, example := range []string{
		"//foundry:model", "//foundry:projection", "models.UserFields()",
		"reports.SummaryFieldsAt(scope)", "UserDraft{}", "models.QueryUsers()",
	} {
		if err := checkMigrationGuide("docs/guides/model-queries.md", []byte(example+link)); err == nil || !strings.Contains(err.Error(), "generated model/query API") {
			t.Fatalf("accepted %q: %v", example, err)
		}
	}
	if err := checkMigrationGuide("docs/guides/model-queries.md", []byte("Use `models.Users.Query()`.")); err == nil || !strings.Contains(err.Error(), "migration link") {
		t.Fatal(err)
	}
	if err := checkMigrationGuide("docs/guides/model-queries.md", []byte("Use `models.Users.Query()`."+link)); err != nil {
		t.Fatal(err)
	}
	if err := checkMigrationGuide("docs/guides/reference.md", []byte("Other teaching.")); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRetainsMigrationGuideRules(t *testing.T) {
	for _, test := range []struct{ text, wantError string }{
		{"Current API without a migration link.", "migration link"},
		{"Use `query.Users`.\n[Migration](orm-migration.md)", "retired API outside a migration before block"},
		{"Use `models.Users.Query()`.\n[Migration](orm-migration.md)", ""},
	} {
		root := testRepository(t)
		writeTestFile(t, root, "docs/guides/model-queries.md", test.text)
		writeTestFile(t, root, "docs/guides/orm-migration.md", "# Migration\n")
		err := checkRepository(root)
		if test.wantError == "" && err != nil || test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
			t.Fatalf("want %q for %q, got %v", test.wantError, test.text, err)
		}
	}
}
