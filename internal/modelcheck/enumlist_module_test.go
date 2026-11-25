package modelcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

const enumModulePath = "foundry.test/enumlists"

// enumModule writes a consumer module that requires the working tree's
// framework, generates its enum and model output and returns its directory.
func enumModule(t *testing.T, files map[string]string) string {
	t.Helper()
	testkit.TrackExternalInputs(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	moduleLine := "module " + frameworkinfo.ModulePath + "\n"
	if !strings.HasPrefix(string(mod), moduleLine) {
		t.Fatal("root go.mod has an unexpected module declaration")
	}
	text := strings.Replace(string(mod), moduleLine, "module "+enumModulePath+"\n", 1)
	text += fmt.Sprintf("\nrequire %s v0.0.0\nreplace %s => %q\n", frameworkinfo.ModulePath, frameworkinfo.ModulePath, root)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"], files["go.sum"] = text, string(sum)
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := generate.Generate(t.Context(), generate.Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// wrongGuessModule is the mistake the check exists for: an enum case was
// added and no migration followed. In the sources, § stands for a backquote.
var wrongGuessModule = map[string]string{
	"enums.go": `package app

//foundry:enum
type Permission string

const (
	PermissionAll          Permission = "*"
	PermissionAdminsRead   Permission = "admins.read"
	PermissionAdminsManage Permission = "admins.manage"
	PermissionPagesExport  Permission = "pages.export"
)
`,
	"models.go": `package app

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=admins var=Admins key=ID
type Admin struct {
	ID          model.ID[Admin]
	Permissions value.JSON[[]Permission] §foundry:"default=database"§
}
`,
	"migrations.go": `package app

import "github.com/weiloon1234/Foundry-Go/database/migrate"

func CreateAdmins() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "202611250001_admins"}, Version: "v0.1.0", SQL: []string{
		§CREATE TABLE admins (id uuid PRIMARY KEY, permissions jsonb NOT NULL DEFAULT '[]'::jsonb,
CONSTRAINT admins_permissions_check CHECK (jsonb_typeof(permissions) = 'array' AND (permissions = '["*"]'::jsonb OR permissions <@ '["admins.read","admins.manage"]'::jsonb)))§,
	}}
}

func All() []migrate.Definition { return []migrate.Definition{CreateAdmins()} }
`,
}

// source turns the § marker of a test source into a backquote.
func source(files map[string]string) map[string]string {
	result := make(map[string]string, len(files))
	for name, text := range files {
		result[name] = strings.ReplaceAll(text, "§", "`")
	}
	return result
}

func TestEnumListWrongGuessMessage(t *testing.T) {
	dir := enumModule(t, source(wrongGuessModule))
	findings, err := Check(t.Context(), dir, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("%d findings: %v", len(findings), findings)
	}
	want := dir + `/enums.go:10:2: [enum-list] enum Permission: case "pages.export" (PermissionPagesExport) can never be stored: it is rejected by constraint admins_permissions_check on admins.permissions, last written at migrations.go:8; ` +
		`either (a) the column should hold the new case: add a new migration that re-declares the constraint with this list: '["*","admins.read","admins.manage","pages.export"]'; ` +
		`or (b) the column deliberately holds only some cases: declare values=PermissionAll|PermissionAdminsRead|PermissionAdminsManage on Admin.Permissions and run foundry generate. ` +
		`Foundry guide: migrations-and-seeding#recipe-enum-lists`
	if got := findings[0].String(); got != want {
		t.Fatalf("message\n got %s\nwant %s", got, want)
	}
}

func TestEnumListWrongGuessFixedByANewMigration(t *testing.T) {
	files := source(wrongGuessModule)
	files["migrations.go"] = strings.Replace(files["migrations.go"], "func All()", `func PagesExport() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "202611250002_pages_export"}, Version: "v0.1.0", Requires: []migrate.Key{CreateAdmins().Key}, SQL: []string{
		§ALTER TABLE admins DROP CONSTRAINT admins_permissions_check§,
		§ALTER TABLE admins ADD CONSTRAINT admins_permissions_check CHECK (jsonb_typeof(permissions) = 'array' AND (permissions = '["*"]'::jsonb OR permissions <@ '["admins.read","admins.manage","pages.export"]'::jsonb))§,
	}}
}

func All()`, 1)
	files["migrations.go"] = strings.ReplaceAll(strings.Replace(files["migrations.go"], "{CreateAdmins()}", "{CreateAdmins(), PagesExport()}", 1), "§", "`")
	findings, err := Check(t.Context(), enumModule(t, files), []string{"./..."})
	if err != nil || len(findings) != 0 {
		t.Fatalf("the newest literal list equals the enum: %v %v", findings, err)
	}
}

var casesModule = map[string]string{
	"enums.go": `package app

//foundry:enum
type Priority string

const (
	Low  Priority = "low"
	High Priority = "high"
)

//foundry:enum
type Phase string

const (
	Draft     Phase = "draft"
	Review    Phase = "review"
	Published Phase = "published"
)

//foundry:enum
type Mode string

const (
	ModeA Mode = "a"
	ModeB Mode = "b"
)

//foundry:enum
type Kind string

const (
	KindX Kind = "x"
	KindY Kind = "y"
)
`,
	"models.go": `package app

import "github.com/weiloon1234/Foundry-Go/model"

//foundry:model table=tickets var=Tickets key=ID
type Ticket struct {
	ID       model.ID[Ticket]
	Priority Priority
}

//foundry:model table=posts var=Posts key=ID
type Post struct {
	ID model.ID[Post]
	// A narrowed column that matches its declared cases.
	Phase Phase §foundry:"values=Draft|Review"§
	// A narrowed column whose list lacks a declared case.
	Stage Phase §foundry:"values=Draft|Review"§
	// A narrowing that names a constant that is not a case.
	Step Phase §foundry:"values=Draft|Missing"§
	// A narrowing that names every case.
	Round Phase §foundry:"values=Draft|Review|Published"§
}

//foundry:model table=orders var=Orders key=ID
type Order struct {
	ID   model.ID[Order]
	Mode Mode
}

//foundry:model table=notes var=Notes key=ID
type Note struct {
	ID   model.ID[Note]
	Kind Kind
}
`,
	"migrations.go": `package app

import "github.com/weiloon1234/Foundry-Go/database/migrate"

func CreateTickets() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "001_tickets"}, Version: "v0.1.0", SQL: []string{
		§CREATE TABLE tickets (id uuid PRIMARY KEY, priority text NOT NULL CHECK (priority IN ('low', 'high', 'urgent')))§,
	}}
}

func CreatePosts() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "002_posts"}, Version: "v0.1.0", SQL: []string{
		§CREATE TABLE posts (id uuid PRIMARY KEY, phase text NOT NULL CHECK (phase IN ('draft', 'review')), stage text NOT NULL CHECK (stage IN ('draft')), step text, round text)§,
	}}
}

func CreateOrders() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "003_orders"}, Version: "v0.1.0", SQL: []string{
		§CREATE TABLE orders (id uuid PRIMARY KEY, mode text NOT NULL CHECK (mode IN ('a', 'b')))§,
		§DO $$ BEGIN ALTER TABLE orders DROP CONSTRAINT orders_mode_check; EXECUTE format('ALTER TABLE orders ADD CONSTRAINT orders_mode_check CHECK (mode IN (%L))', 'a'); END $$§,
	}}
}

func CreateNotes() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "004_notes"}, Version: "v0.1.0", SQL: []string{
		§CREATE TABLE notes (id uuid PRIMARY KEY, kind text NOT NULL)§,
	}}
}

func suffix() string { return " DEFAULT 'x'" }

func Dynamic() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "005_dynamic"}, Version: "v0.1.0", SQL: []string{
		"ALTER TABLE tickets ADD COLUMN note text" + suffix(),
	}}
}

func Forgotten() migrate.Definition {
	return migrate.Definition{Key: migrate.Key{Origin: "app", ID: "006_forgotten"}, Version: "v0.1.0", SQL: []string{
		§ALTER TABLE notes ADD CONSTRAINT notes_kind_check CHECK (kind IN ('x'))§,
	}}
}

func All() []migrate.Definition {
	return []migrate.Definition{CreateTickets(), CreatePosts(), CreateOrders(), CreateNotes(), Dynamic()}
}
`,
}

func TestEnumListCases(t *testing.T) {
	dir := enumModule(t, source(casesModule))
	findings, err := Check(t.Context(), dir, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range findings {
		got = append(got, strings.TrimPrefix(d.String(), dir+"/"))
	}
	const guide = ". Foundry guide: migrations-and-seeding#recipe-enum-lists"
	wants := []string{
		`enums.go:16:2: [enum-list] enum Phase: case "review" (Review) can never be stored: it is rejected by the unnamed CHECK on posts.stage, last written at migrations.go:13; either (a) the column should hold the case: add a new migration that re-declares the constraint with this list: 'draft', 'review'; or (b) the column should not hold it: remove it from values= on Post.Stage and run foundry generate` + guide,
		`migrations.go:34: [enum-list] migration: cannot be read by the enum list check: a SQL statement is built at run time, not written as a string constant; write the key and the SQL as constants, and a constraint as literal ALTER TABLE DROP CONSTRAINT and ADD CONSTRAINT statements` + guide,
		`migrations.go:38:6: [migration-unregistered] migration Forgotten (app/006_forgotten): is never listed with the application's other migrations, so it never runs; add Forgotten() to the list of migrate.Definition values the application registers, or delete the file. Foundry guide: migrations-and-seeding#recipe-register-migration`,
		`models.go:8:2: [enum-list] Ticket.Priority: the unnamed CHECK on tickets.priority, last written at migrations.go:7 holds "urgent", which is not an enum case; add a new migration that re-declares the constraint with this list: 'low', 'high'` + guide,
		`models.go:19:2: [enum-list] Post.Step: values names Missing, which is not a case of enum Phase; name only cases of the enum, written as their Go constant names separated by |` + guide,
		`models.go:21:2: [enum-list] Post.Round: values names every case of enum Phase; remove the values option; it narrows a column to some cases` + guide,
		`models.go:27:2: [enum-list] orders.mode: the CHECK constraints on this enum column cannot be read statically: the unnamed CHECK was added at migrations.go:19, and a DO block at migrations.go:20 may have changed it; write the constraint again in a new migration as literal ALTER TABLE orders DROP CONSTRAINT <constraint name, shown by \d orders> and ALTER TABLE orders ADD CONSTRAINT <constraint name, shown by \d orders> CHECK (...) statements whose test only compares the column with literals, such as IN (...), = ANY (ARRAY[...]) or <@ '[...]'; the newest literal constraint is the one evaluated; read the constraint's name from psql's \d orders` + guide,
	}
	if strings.Join(got, "\n") != strings.Join(wants, "\n") {
		t.Fatalf("findings\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wants, "\n"))
	}
}
