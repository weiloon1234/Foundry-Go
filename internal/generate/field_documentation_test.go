package generate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFieldDocumentationPreservesSourceAndIsReproducible(t *testing.T) {
	for name, input := range map[string]string{
		"ordinary":    "package sample\ntype User struct {\n Key int\n Email string\n}\n",
		"compact":     "package sample\ntype User struct{Key int; Email string}\n",
		"leading":     "package sample\ntype User struct {\n Key int\n // Email is the delivery address.\n Email string\n}\n",
		"trailing":    "package sample\ntype User struct {\n Key int\n Email string // Original delivery address.\n}\n",
		"block":       "package sample\ntype User struct {\n Key int\n /* Email is the delivery address. */\n Email string\n}\n",
		"multi field": "package sample\ntype User struct{Key int; Email, Other string}\n",
		"unicode":     "package sample\n// А note.\ntype User struct{Key int; Email string}\n",
	} {
		t.Run(name, func(t *testing.T) {
			p := &packageInput{files: []source{{name: "models.go", data: []byte(input)}}}
			meta := metadata{models: []model{{name: "User", table: "users", fields: []field{{name: "Email", column: "email_address", accessor: "AccessEmail", mutator: "MutateEmail"}}}}}
			updates, err := planFieldDocumentation(p, &meta)
			if err != nil {
				t.Fatal(err)
			}
			output := updates["models.go"]
			for _, want := range []string{"User.AccessEmail", "User.MutateEmail", "UserDraft.SetEmail", "users.email_address", "stored", "DTO"} {
				if !bytes.Contains(output, []byte(want)) {
					t.Fatalf("missing %s: %s", want, output)
				}
			}
			if err := validateFieldDocumentationChange([]byte(input), output); err != nil {
				t.Fatal(err)
			}
			if name == "trailing" && !bytes.Contains(output, []byte("Existing field documentation: Original delivery address.")) {
				t.Fatal("leading notices would hide original trailing field help")
			}
			p.files[0].data = output
			if repeated, err := planFieldDocumentation(p, &meta); err != nil || len(repeated) != 0 {
				t.Fatalf("not reproducible: %s, %v", repeated["models.go"], err)
			}
			meta.models[0].fields[0].accessor, meta.models[0].fields[0].mutator = "", ""
			removed, err := planFieldDocumentation(p, &meta)
			if err != nil || bytes.Contains(removed["models.go"], []byte(fieldNotePrefix)) {
				t.Fatalf("obsolete notices retained: %v", err)
			}
			if err := validateFieldDocumentationChange([]byte(input), removed["models.go"]); err != nil {
				t.Fatalf("notice removal changed handwritten data: %v", err)
			}
		})
	}
}

func TestFieldDocumentationRejectsExecutableAndUserCommentChanges(t *testing.T) {
	before := []byte("package sample\n// Important note.\ntype User struct { Email string }\n")
	for name, after := range map[string][]byte{
		"field type": bytes.ReplaceAll(before, []byte("string"), []byte("int")),
		"human note": bytes.ReplaceAll(before, []byte("Important"), []byte("Changed")),
		"new code":   append(bytes.Clone(before), []byte("func changed() {}\n")...),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateFieldDocumentationChange(before, after); err == nil {
				t.Fatal("non-documentation change accepted")
			}
		})
	}
}

func TestFieldDocumentationKeepsSameNamedModelFieldsDistinct(t *testing.T) {
	p := &packageInput{files: []source{{name: "models.go", data: []byte("package sample\ntype User struct{Email string}\ntype Contact struct{Email string}\n")}}}
	meta := metadata{models: []model{
		{name: "User", table: "users", fields: []field{{name: "Email", column: "login_email", accessor: "AccessEmail"}}},
		{name: "Contact", table: "contacts", fields: []field{{name: "Email", column: "delivery_email", mutator: "MutateEmail"}}},
	}}
	updates, err := planFieldDocumentation(p, &meta)
	if err != nil {
		t.Fatal(err)
	}
	source := string(updates["models.go"])
	user, contact, ok := strings.Cut(source, "type Contact")
	if !ok || !strings.Contains(user, "[User.AccessEmail]") || strings.Contains(user, "MutateEmail") || !strings.Contains(contact, "[Contact.MutateEmail]") || strings.Contains(contact, "AccessEmail") {
		t.Fatalf("field names crossed model owners: %s", source)
	}
}

func TestModelAccessorGenerationAndFieldNoticeFreshness(t *testing.T) {
	input := `package sample
type DisplayEmail string
type Helper string
func (Helper) AccessDomain(d UserDraft) UserDraft { return d }
//foundry:model table=users primary=Key
type User struct { Key int; Email string }
func (u User) AccessKey() (string,error) { return "display",nil }
func (u User) AccessEmail() (DisplayEmail,error) { _ = UserDraft{}; return DisplayEmail(u.Email),nil }
func (User) MutateEmail(v string)(string,error) { return v,nil }
//foundry:enum
type State string
const Ready State = "ready"
//foundry:projection
type UserRow struct { Email string }
//foundry:path pattern=/users/{key}
type UserPath struct { Key int }
//foundry:query
type SearchInput struct { Name string }
//foundry:multipart
type UploadInput struct { Name string }
//foundry:dto
type UserResponse struct { Email string }
`
	dir := fixture(t, input)
	if _, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
	source := snapshotFile(t, dir, "models.go")
	first := generatedSnapshot(t, dir)
	for _, name := range []string{"user_foundry.gen.go", "state_foundry.gen.go", "user_row_foundry.gen.go", "user_path_foundry.gen.go", "search_input_foundry.gen.go", "upload_input_foundry.gen.go", "user_response_foundry.gen.go"} {
		if !strings.Contains(first[name], "// Source: models.go.\n") {
			t.Fatal("source file lost or line number retained")
		}
	}
	if !strings.Contains(first["user_foundry.gen.go"], "Custom getter: [User.AccessEmail]") || !strings.Contains(string(source.data), fieldNotePrefix) {
		t.Fatal("model/descriptors/drafts lost their shared field metadata")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
	if report, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: true}); err != nil || len(report.Written) != 0 || !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatalf("repeat generation changed source locations or output: %+v, %v", report, err)
	}
	// A removed notice is stale independently of the compiled generated files.
	clean, err := stripFieldNotes(source.data)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "models.go", string(clean))
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err == nil || !strings.Contains(err.Error(), "stale") || !strings.Contains(err.Error(), "models.go: "+string(StaleFieldNotes)) {
		t.Fatalf("missing field documentation was accepted: %v", err)
	}
	// Without the opt-in, generation never reads or rewrites handwritten notes.
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatalf("default generation inspected handwritten notes: %v", err)
	}
	if report, err := Generate(t.Context(), Options{Dir: dir}); err != nil || len(report.Written) != 0 {
		t.Fatalf("default generation rewrote handwritten source: %+v %v", report, err)
	}
	if current := snapshotFile(t, dir, "models.go"); !bytes.Equal(current.data, clean) {
		t.Fatal("check changed handwritten source")
	}
}

func TestModelAccessorRejectsInvalidSignatures(t *testing.T) {
	for name, method := range map[string]string{
		"pointer":          "func(*User)AccessEmail()(string,error){return \"\",nil}",
		"argument":         "func(User)AccessEmail(v string)(string,error){return v,nil}",
		"missing error":    "func(User)AccessEmail()string{return \"\"}",
		"untyped result":   "func(User)AccessEmail()(any,error){return nil,nil}",
		"misspelled field": "func(User)AccessEamil()(string,error){return \"\",nil}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:model table=users primary=Key\ntype User struct{Key int;Email string}\n"+method)
			before := snapshotFile(t, dir, "models.go")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "model accessor") {
				t.Fatalf("invalid getter accepted: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 || !bytes.Equal(before.data, snapshotFile(t, dir, "models.go").data) {
				t.Fatal("invalid getter changed source or generated output")
			}
		})
	}
}

func TestFieldDocumentationPublicationRollsBackTogether(t *testing.T) {
	dir := fixture(t, "package sample\ntype User struct{Email string}\n")
	before := snapshotFile(t, dir, "models.go")
	after := []byte("package sample\ntype User struct{\n" + fieldNotePrefix + "Custom getter.\nEmail string}\n")
	plan := writePlan{changes: map[string][]byte{"models.go": after, manifestName: []byte("next")}, before: map[string]oldFile{"models.go": before, manifestName: {}}, fieldNotes: map[string]bool{"models.go": true}}
	p := &packageInput{dir: dir, files: []source{{name: "models.go", data: before.data}}}
	failure := errors.New("injected final publication failure")
	err := publishWithRename(t.Context(), p, plan, func(from, to string) error {
		if filepath.Base(to) == manifestName {
			return failure
		}
		return os.Rename(from, to)
	})
	if !errors.Is(err, failure) || state(snapshotFile(t, dir, "models.go")) != state(before) {
		t.Fatalf("publication did not restore handwritten source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); !os.IsNotExist(err) {
		t.Fatalf("successful rollback retained staging: %v", err)
	}
}

func TestFieldDocumentationRecoveryValidatesSource(t *testing.T) {
	if !automaticRecovery {
		t.Skip("recovery adapter unavailable")
	}
	for _, complete := range []bool{false, true} {
		for _, unsafe := range []bool{false, true} {
			t.Run(fmt.Sprintf("complete=%t/unsafe=%t", complete, unsafe), func(t *testing.T) {
				dir := fixture(t, "package sample\ntype User struct{Email string}\n")
				before := snapshotFile(t, dir, "models.go")
				after := []byte("package sample\ntype User struct{\n" + fieldNotePrefix + "Custom getter.\nEmail string}\n")
				if unsafe {
					after = bytes.ReplaceAll(after, []byte("Email string"), []byte("Email int"))
				}
				plan := writePlan{changes: map[string][]byte{"models.go": after, manifestName: []byte("next")}, before: map[string]oldFile{"models.go": before, manifestName: {}}, guards: []string{"."}, fieldNotes: map[string]bool{"models.go": true}}
				// Stage directly to test recovery's independent validation, even
				// for an invalid journal that normal publication would reject.
				if err := prepareStaging(dir, plan); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(dir, lockName, stageName("new", "models.go")), filepath.Join(dir, "models.go")); err != nil {
					t.Fatal(err)
				}
				if complete {
					if err := os.Rename(filepath.Join(dir, lockName, stageName("new", manifestName)), filepath.Join(dir, manifestName)); err != nil {
						t.Fatal(err)
					}
				}
				result, err := Recover(t.Context(), dir)
				if unsafe {
					if err == nil || !strings.Contains(err.Error(), "preserve handwritten") {
						t.Fatalf("unsafe recovery accepted: %+v %v", result, err)
					}
					return
				}
				if err != nil || result.Kept != complete || result.RolledBack == complete {
					t.Fatalf("recovery: %+v %v", result, err)
				}
				want := before.data
				if complete {
					want = after
				}
				if !bytes.Equal(snapshotFile(t, dir, "models.go").data, want) {
					t.Fatal("recovery lost source")
				}
			})
		}
	}
}
