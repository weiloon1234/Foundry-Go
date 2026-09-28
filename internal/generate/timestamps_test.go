package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModelTimestampGeneration(t *testing.T) {
	source := `package sample
import (
 "time"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/temporal"
)
//foundry:model table=users
type User struct{ID model.ID[User];CreatedAt time.Time CREATED;UpdatedAt temporal.DateTime UPDATED}
func(User)MutateUpdatedAt(v temporal.DateTime)(temporal.DateTime,error){return v,nil}
var _ = UserDraft{}.SetCreatedAt(time.Time{}).SetUpdatedAt(temporal.DateTime{})
var _ = QueryUsers().Where(UserFields().UpdatedAt.Gt(temporal.DateTime{}))
`
	source = strings.ReplaceAll(source, "CREATED", "`foundry:\"column=made_on\"`")
	source = strings.ReplaceAll(source, "UPDATED", "`foundry:\"column=changed_on\"`")
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["user_foundry.gen.go"]
	for _, want := range []string{`.WithTimestamps("made_on", "changed_on")`, "Managed creation timestamp", "Managed update timestamp", "MutateUpdatedAt"} {
		if !strings.Contains(output, want) {
			t.Fatal("missing timestamp contract", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("timestamp output changed on repeat")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestModelTimestampOptOutAndMissingPair(t *testing.T) {
	for _, source := range []string{
		"//foundry:model table=users primary=ID timestamps=false\ntype User struct{ID int;CreatedAt string;UpdatedAt string}",
		"//foundry:model table=users primary=ID\ntype User struct{ID int;CreatedAt string}",
	} {
		dir := fixture(t, "package sample\n"+source)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		output := generatedSnapshot(t, dir)["user_foundry.gen.go"]
		if strings.Contains(output, "WithTimestamps") || strings.Contains(output, "Managed creation timestamp") {
			t.Fatal("ordinary fields acquired timestamp behavior")
		}
	}
}

func TestModelTimestampNoticesFollowOptOut(t *testing.T) {
	dir := fixture(t, `package sample
import "time"
//foundry:model table=users primary=ID
type User struct{
 ID int
 // Creation history belongs to this record.
 CreatedAt time.Time
 UpdatedAt time.Time
}
func(User)MutateUpdatedAt(v time.Time)(time.Time,error){return v,nil}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "models.go")
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(initial), "Managed creation timestamp") || !strings.Contains(string(initial), "Managed update timestamp") {
		t.Fatal("handwritten fields lack timestamp notices")
	}
	updated := strings.Replace(string(initial), "table=users primary=ID", "table=users primary=ID timestamps=false", 1)
	write(t, dir, "models.go", updated)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil {
		t.Fatal("timestamp opt-out left generated output current")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Managed creation timestamp") || strings.Contains(string(data), "Managed update timestamp") {
		t.Fatal("timestamp opt-out retained obsolete field notices")
	}
	if !strings.Contains(string(data), "Creation history belongs to this record.") || !strings.Contains(string(data), "Custom setter") {
		t.Fatal("timestamp opt-out removed handwritten or mutator documentation")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestModelTimestampRejectsInvalidDeclarations(t *testing.T) {
	for name, source := range map[string]string{
		"option":         "//foundry:model table=users primary=ID timestamps=yes\ntype User struct{ID int}",
		"missing":        "//foundry:model table=users primary=ID timestamps=true\ntype User struct{ID int;CreatedAt time.Time}",
		"wrong type":     "//foundry:model table=users primary=ID\ntype User struct{ID int;CreatedAt string;UpdatedAt time.Time}",
		"nullable":       "//foundry:model table=users primary=ID\ntype User struct{ID int;CreatedAt time.Time;UpdatedAt value.Nullable[time.Time]}",
		"primary":        "//foundry:model table=users primary=CreatedAt\ntype User struct{CreatedAt time.Time;UpdatedAt time.Time}",
		"distinct input": "//foundry:model table=users primary=ID\ntype User struct{ID int;CreatedAt time.Time;UpdatedAt time.Time}\ntype FreshTime struct{T time.Time}\nfunc(User)MutateUpdatedAt(v FreshTime)(time.Time,error){return v.T,nil}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\nimport(\"time\";\"github.com/weiloon1234/Foundry-Go/value\")\nvar _ = time.Time{}\nvar _ = value.Set(1)\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid timestamp declaration generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid timestamp declaration published output")
			}
		})
	}
}
