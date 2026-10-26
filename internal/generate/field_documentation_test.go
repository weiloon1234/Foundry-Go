package generate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestFieldDocumentationRetiresOnlyManagedNotes(t *testing.T) {
	dir := fixture(t, `package sample
//foundry:dto
type Request struct {
 // Customer supplied email.
 // Foundry field behavior (generated): Historical accessor.
 Email string
}
`)
	before := snapshotFile(t, dir, "models.go").data
	if _, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
	after := snapshotFile(t, dir, "models.go").data
	if bytes.Contains(after, []byte(fieldNotePrefix)) || !bytes.Contains(after, []byte("Customer supplied email.")) {
		t.Fatal("incorrect managed-note retirement")
	}
	if err := validateFieldDocumentationChange(before, after); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
}
