package main

import (
	"strings"
	"testing"
)

func TestCurrentModelGuideRejectsRetiredTeaching(t *testing.T) {
	const link = "\n[Migration](model-declarations.md)\n"
	for _, example := range []string{
		"models.UserFields()",
		"reports.SummaryFieldsAt(scope)", "UserDraft{}", "models.QueryUsers()",
	} {
		if err := checkModelGuide("docs/guides/model-queries.md", []byte(example+link)); err == nil || !strings.Contains(err.Error(), "generated model/query API") {
			t.Fatalf("accepted %q: %v", example, err)
		}
	}
	if err := checkModelGuide("docs/guides/model-queries.md", []byte("Use `models.Users.Query()`.")); err != nil {
		t.Fatal(err)
	}
	if err := checkModelGuide("docs/guides/model-queries.md", []byte("Use `models.Users.Query()`."+link)); err != nil {
		t.Fatal(err)
	}
	if err := checkModelGuide("docs/guides/reference.md", []byte("Other teaching.")); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRetainsRetirementRulesWithoutMigrationLink(t *testing.T) {
	for _, test := range []struct{ text, wantError string }{
		{"Current API without a migration link.", ""},
		{"Use `query.Users`.\n[Migration](model-declarations.md)", "retired API in teaching"},
		{"Use `models.Users.Query()`.\n[Migration](model-declarations.md)", ""},
	} {
		root := testRepository(t)
		writeTestFile(t, root, "docs/guides/model-queries.md", test.text)
		writeTestFile(t, root, "docs/guides/model-declarations.md", "# Migration\n")
		err := checkRepository(root)
		if test.wantError == "" && err != nil || test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
			t.Fatalf("want %q for %q, got %v", test.wantError, test.text, err)
		}
	}
}

func TestCurrentModelGuideAllowsModelDirective(t *testing.T) {
	if err := checkModelGuide("docs/guides/model-queries.md", []byte("//foundry:model table=users var=Users key=ID\ntype User struct { ID int }")); err != nil {
		t.Fatal(err)
	}
}

func TestModelGuideDeclarationSpelling(t *testing.T) {
	for _, code := range []string{
		"type UserTable struct { orm.Table[User, int] }",
		"type UserTable = orm.Table[User, int]",
		"type UserTable struct { *orm.Table[User, int] }",
		"var Users = orm.Define(\"users\", bindUser)",
		"var userIdentity = orm.IdentityOf(Users.Table, Users.Key())",
		"func (u User) FoundryIdentity() (model.Identity, error) { return userIdentity.ReferenceOf(u).Identity() }",
		"import table \"github.com/weiloon1234/Foundry-Go/database/orm\"\ntype UserTable struct { table.Table[User, int] }",
	} {
		for _, name := range []string{"docs/guides/model-declarations.md", "database/orm/README.md", "database/AGENTS.md"} {
			if err := checkModelGuide(name, []byte("```go\n"+code+"\n```\n")); err == nil || !strings.Contains(err.Error(), "//foundry:model") {
				t.Fatalf("accepted %s: %s: %v", name, code, err)
			}
		}
	}
	for _, code := range []string{
		"//foundry:model table=users var=Users key=ID\ntype User struct { ID int }",
		"func identity(user User) model.Reference[User, int] { return orm.IdentityOf(Users.Table, Users.Key()).ReferenceOf(user) }",
		"var Owner = extensions.DefineORMOwner(\"users\", orm.IdentityOf(Users.Table, Users.Key()), options)",
		"type UserRow struct { rows.Row[User]; ID rows.Field[User, int] }",
		"// orm.Define is called by generated code\nvar note = \"orm.Define(\"",
	} {
		if err := checkModelGuide("docs/guides/model-declarations.md", []byte("```go\n"+code+"\n```\n")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLowLevelModelReferenceIsOneSourceBoundBlock(t *testing.T) {
	block := "<!-- compiled-source: tests/fixtures/example_test.go#binding -->\n```go\nvar Users = orm.Define(\"users\", bindUser)\n```\n"
	if err := checkModelGuide("database/orm/README.md", []byte(modelReferenceMarker+"\n"+block)); err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{modelReferenceMarker, modelReferenceMarker + "\n```go\nx\n```", modelReferenceMarker + "\n" + block + block, modelReferenceMarker + "\n" + strings.ReplaceAll(block, "example_test.go", "example.go")} {
		if err := checkModelGuide("database/orm/README.md", []byte(doc)); err == nil {
			t.Fatal("reference escaped its source-bound block")
		}
	}
}

func TestRepositoryChecksModelDeclarationTeaching(t *testing.T) {
	for _, name := range []string{"docs/guides/declarations.md", "database/orm/README.md", "database/AGENTS.md"} {
		root := testRepository(t)
		writeTestFile(t, root, name, "```go\nvar Users = orm.Define(\"users\", bindUser)\n```\n")
		if err := checkRepository(root); err == nil || !strings.Contains(err.Error(), "handwritten model declaration teaching") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestHistoricalLabelCannotExemptGeneratedExamples(t *testing.T) {
	doc := "<!-- orm-example: inputs-before -->\n```go\nreturn oldmutators.MemberDraft{}.SetEmail(input)\n```\n"
	if err := checkModelGuide("docs/guides/orm-migration.md", []byte(doc)); err == nil {
		t.Fatal("historical label exempted generated examples")
	}
}

func TestRetiredGeneratedModelDiagnosticKeepsLine(t *testing.T) {
	err := checkModelGuide("guide.md", []byte("# Guide\n\nText.\n\nUse models.UserFields().\n"))
	if err == nil || !strings.Contains(err.Error(), "guide.md:5: guide teaches a retired generated model/query API; declare models with //foundry:model") {
		t.Fatalf("lost located repair: %v", err)
	}
}
