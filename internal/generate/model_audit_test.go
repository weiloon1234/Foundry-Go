package generate

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestModelAuditGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import("github.com/weiloon1234/Foundry-Go/value")
type NaturalKey string
func(NaturalKey)MarshalJSON()([]byte,error){panic("presentation key must not run")}
//foundry:model table=members primary=Key
type Member struct{Key NaturalKey;Email string;PasswordHash string;Note value.Nullable[string]}
func(Member)MarshalJSON()([]byte,error){panic("model presentation must not run")}
func(Member)AccessEmail()(string,error){panic("getter must not run")}
//foundry:model table=other_members primary=ID
type Other struct{ID int}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, wanted := range []string{"MemberAuditPolicy", "MemberAuditFieldSet", "MemberAuditFields", "MemberAuditing", "audit/record"} {
		found := false
		for _, source := range first {
			found = found || strings.Contains(source, wanted)
		}
		if !found {
			t.Fatal("missing generated audit contract", wanted)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("audit generation is not deterministic")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "audit_test.go", `package sample
import("errors";"strings";"testing";"github.com/weiloon1234/Foundry-Go/audit/record";"github.com/weiloon1234/Foundry-Go/database/lifecycle";"github.com/weiloon1234/Foundry-Go/fault";"github.com/weiloon1234/Foundry-Go/model";"github.com/weiloon1234/Foundry-Go/value")
func TestGeneratedAudit(t *testing.T){
 before:=Member{Key:"natural-key",Email:"stored@example.test",PasswordHash:"old-secret",Note:value.Of("private-note")}
 after:=before;after.PasswordHash="new-secret"
 changes,err:=CompareMember(value.Set(before),value.Set(after),MemberDraft{}.SetEmail(after.Email).SetPasswordHash(after.PasswordHash));if err!=nil{t.Fatal(err)}
 if _,err:=changes.Audit(MemberAuditPolicy{});!errors.Is(err,fault.Missing){t.Fatal("standalone comparison became a lifecycle audit",err)}
 // Exercise the generated capture adapter with the same metadata set by its
 // generated normal-write pipeline. PostgreSQL observer integration is separate.
 changes.operation=value.Set(lifecycle.Update)
 policy:=MemberAuditPolicy{Note:record.Exclude}
 var declaration record.Declaration=MemberAuditing(policy);_=declaration
 var history record.Model[Member,NaturalKey]
 history,err=changes.Audit(policy);if err!=nil{t.Fatal(err)}
 var subject model.Reference[Member,NaturalKey]
 subject,err=history.Subject();if err!=nil||subject.Key()!=before.Key{t.Fatal("stored subject changed",err)}
 fields,err:=MemberAuditFields(history);if err!=nil{t.Fatal(err)}
 email,present:=fields.Email.Get();if !present||!email.Assigned()||email.Changed(){t.Fatal("assignment/dirty flags changed")}
 emailValue,err:=email.After().Get();if err!=nil{t.Fatal(err)}
 stored,present:=emailValue.Get();if !present||stored!=after.Email{t.Fatal("getter or presentation replaced storage")}
 password,present:=fields.PasswordHash.Get();if !present||password.After().State()!=record.Redacted{t.Fatal("credential was not redacted")}
 if fields.Note.IsSet(){t.Fatal("excluded field remained")}
 payload,err:=history.Entry().Payload();if err!=nil||strings.Contains(payload,"secret")||strings.Contains(payload,"private-note"){t.Fatal("audit exposed excluded/sensitive data",err)}
 restored,err:=record.RestoreModel((Member{}).FoundryReference(),history.Entry());if err!=nil{t.Fatal(err)}
 if _,err:=MemberAuditFields(restored);err!=nil{t.Fatal(err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated audit consumer: %v\n%s", err, output)
	}
}

func TestModelAuditRejectsDeclarationCollisionsBeforePublication(t *testing.T) {
	for _, declaration := range []string{
		"type MemberAuditPolicy struct{}",
		"type MemberAuditFieldSet struct{}",
		"func MemberAuditFields(){}",
		"func MemberAuditing(){}",
		"func(MemberChanges)Audit(){}",
	} {
		dir := fixture(t, "package sample\n//foundry:model table=members primary=ID\ntype Member struct{ID int}\n"+declaration)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
			t.Fatal("conflicting audit declaration accepted", declaration)
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("invalid audit declaration published output")
		}
	}
}

func TestModelAuditGeneratedLocalsDoNotHideStoredTypes(t *testing.T) {
	dir := fixture(t, `package sample
type policy string
type captured string
type history string
type fields string
type builder string
type writer string
type view string
//foundry:model table=entries primary=Key
type Entry struct{Key policy;Captured captured;History history;Fields fields;Builder builder;Writer writer;View view}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated audit names: %v\n%s", err, output)
	}
}
