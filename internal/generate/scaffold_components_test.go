package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComponentScaffoldsGenerateAndCompileInFreshConsumer(t *testing.T) {
	dir := fixture(t, "package sample\n\n// Member and Note are policy subject/resource types.\ntype Member struct{ ID string }\ntype Note struct{ Owner string }\n")
	for _, options := range []ScaffoldOptions{
		{Kind: EndpointScaffold, Name: "CreateNote", ID: "notes.create", Method: "POST", Path: "/notes"},
		{Kind: EndpointScaffold, Name: "ListNotes", ID: "notes.list", Method: "GET", Path: "/notes"},
		{Kind: EnumScaffold, Name: "NoteState", Cases: []string{"draft", "published"}},
		{Kind: MiddlewareScaffold, Name: "Audit", ID: "notes.audit"},
		{Kind: RuleScaffold, Name: "Slug", ID: "notes.slug"},
		{Kind: EventScaffold, Name: "NotePublished", ID: "notes.published"},
		{Kind: ListenerScaffold, Name: "IndexNote", ID: "notes.index", Event: "NotePublished"},
		{Kind: PolicyScaffold, Name: "UpdateNote", ID: "notes.update", Subject: "Member", Resource: "Note"},
		{Kind: NotificationScaffold, Name: "NoteShared", ID: "notes.shared"},
		{Kind: MigrationScaffold, Name: "CreateNotes", ID: "notes.create_table", Origin: "app", Version: "v1.0.0", Create: "notes"},
	} {
		options.Dir = dir
		path, err := Scaffold(t.Context(), options)
		if err != nil {
			t.Fatal(options.Kind, err)
		}
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), generatedHeader) {
			t.Fatal("scaffold is missing or generator-owned", err)
		}
		if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
			t.Fatal(options.Kind, err)
		}
		if _, err := Scaffold(t.Context(), options); err == nil {
			t.Fatal("existing scaffold overwritten", options.Kind)
		}
	}
	for _, name := range []string{"create_note_request_dto.go", "create_note_response_dto.go", "list_notes_response_dto.go", "note_shared_payload_dto.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("contract scaffold missing", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "list_notes_request_dto.go")); err == nil {
		t.Fatal("GET endpoint received a request body")
	}
	migration, err := os.ReadFile(filepath.Join(dir, "create_notes_migration.go"))
	if err != nil || !strings.Contains(string(migration), "CREATE TABLE notes (") {
		t.Fatal("create-table migration template missing", err)
	}
	write(t, dir, "components_test.go", `package sample
import (
 "context"
 "errors"
 "testing"
 "github.com/weiloon1234/Foundry-Go/fault"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)
func TestComponentScaffolds(t *testing.T){
 if err:=CreateNoteEndpoint().Validate();err!=nil{t.Fatal(err)}
 if err:=ListNotesEndpoint().Validate();err!=nil{t.Fatal(err)}
 if _,err:=HandleCreateNote(context.Background(),foundryhttp.Input[foundryhttp.NoPath,foundryhttp.NoQuery,CreateNoteRequest]{});!errors.Is(err,fault.Invalid){t.Fatal("unfinished endpoint succeeded",err)}
 if NoteStateDraft!="draft"||NoteStatePublished!="published"{t.Fatal("enum cases")}
 if err:=AuditMiddleware().Validate();err!=nil{t.Fatal(err)}
 if err:=SlugRule().Validate();err!=nil{t.Fatal(err)}
 if err:=NotePublishedTopic().Validate();err!=nil{t.Fatal(err)}
 if _,err:=NotePublishedTopic().Declare(IndexNoteListener());err!=nil{t.Fatal(err)}
 if err:=UpdateNotePolicy().Validate();err!=nil{t.Fatal(err)}
 if err:=NoteSharedNotification().Validate();err!=nil{t.Fatal(err)}
 if len(CreateNotes().SQL)!=1{t.Fatal("create-table migration has no SQL")}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fresh component scaffolds: %s %v", output, err)
	}
}

func TestComponentScaffoldOptionsAreValidatedFirst(t *testing.T) {
	for _, options := range []ScaffoldOptions{
		{Kind: EndpointScaffold, Name: "Create", ID: "notes.create", Method: "TRACE", Path: "/notes"},
		{Kind: EndpointScaffold, Name: "Create", ID: "notes.create", Method: "POST", Path: "/notes/{id}"},
		{Kind: EndpointScaffold, Name: "Create", ID: "notes.create", Method: "POST", Path: "notes"},
		{Kind: EnumScaffold, Name: "State", Cases: []string{"Draft"}},
		{Kind: EnumScaffold, Name: "State", Cases: []string{"draft", "draft"}},
		{Kind: EnumScaffold, Name: "State"},
		{Kind: EnumScaffold, Name: "State", ID: "ignored", Cases: []string{"draft"}},
		{Kind: ListenerScaffold, Name: "Index", ID: "notes.index", Event: "lowercase"},
		{Kind: PolicyScaffold, Name: "Update", ID: "notes.update", Subject: "Member"},
		{Kind: MiddlewareScaffold, Name: "Audit", ID: "notes.audit", Path: "/ignored"},
		{Kind: MigrationScaffold, Name: "Create", ID: "notes.create", Origin: "app", Version: "v1", Create: "bad; table"},
		{Kind: RuleScaffold, Name: "Slug", ID: "notes.slug", Create: "notes"},
	} {
		if err := ValidateScaffold(options); err == nil {
			t.Fatalf("invalid scaffold options accepted: %+v", options)
		}
	}
}
