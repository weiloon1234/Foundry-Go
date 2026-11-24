package generate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Each accepted declaration and each correction advertised by a rejection is
// compiled together and checked by the real ORM, without opening a database.
func TestModelTimestampConventions(t *testing.T) {
	type correction struct{ fields, options string }
	type scenario struct {
		name, fields, options, message string
		fixes                          []correction
	}
	field := func(name, typ, tag string) string {
		if tag == "" {
			return name + " " + typ + ";"
		}
		return name + " " + typ + " `foundry:\"" + tag + "\"`;"
	}
	var cases []scenario
	for _, typ := range []string{"time.Time", "temporal.DateTime", "value.Nullable[temporal.DateTime]", "value.Nullable[time.Time]", "temporal.Date", "temporal.Time", "temporal.LocalDateTime", "temporal.Interval", "string"} {
		instant := typ == "time.Time" || typ == "temporal.DateTime"
		temporal := typ != "string" && typ != "temporal.Interval"
		for _, name := range []string{"CreatedAt", "UpdatedAt"} {
			raw := field(name, typ, "")
			if temporal {
				cases = append(cases, scenario{name + "/" + typ, raw, "", "declare role=time for an ordinary time column", []correction{{field(name, typ, "role=time"), ""}}})
			} else {
				cases = append(cases, scenario{name + "/" + typ, raw, "", "", nil})
			}
		}
		for _, role := range []string{"", "role=time"} {
			if !temporal && role != "" {
				continue
			}
			pair := field("CreatedAt", typ, role) + field("UpdatedAt", typ, role)
			fixes := []correction{{field("CreatedAt", typ, "") + field("UpdatedAt", typ, ""), "timestamps=none"}}
			message := "use timestamps=none on the model"
			if instant {
				message = "declare role=created-at on CreatedAt and role=updated-at on UpdatedAt, or " + message
				fixes = append(fixes, correction{field("CreatedAt", typ, "role=created-at") + field("UpdatedAt", typ, "role=updated-at"), ""})
			}
			cases = append(cases, scenario{"pair/" + typ + "/" + role, pair, "", message, fixes})
			deletedFixes := []correction{{field("DeletedAt", typ, ""), "softdeletes=none"}}
			message = "use softdeletes=none on the model"
			if typ == "value.Nullable[temporal.DateTime]" || typ == "value.Nullable[time.Time]" {
				message = "declare role=soft-delete on DeletedAt, or " + message
				deletedFixes = append(deletedFixes, correction{field("DeletedAt", typ, "role=soft-delete"), ""})
			}
			cases = append(cases, scenario{"deleted/" + typ + "/" + role, field("DeletedAt", typ, role), "", message, deletedFixes})
		}
	}
	for _, typ := range []string{"temporal.DateTime", "value.Nullable[time.Time]"} {
		cases = append(cases,
			scenario{"default single/" + typ, field("CreatedAt", typ, "default=database"), "", "declare role=time", []correction{{field("CreatedAt", typ, "role=time,default=database"), ""}}},
			scenario{"default pair/" + typ, field("CreatedAt", typ, "default=database") + field("UpdatedAt", typ, ""), "", "use timestamps=none on the model", []correction{{field("CreatedAt", typ, "default=database") + field("UpdatedAt", typ, ""), "timestamps=none"}}},
			scenario{"default deleted/" + typ, field("DeletedAt", typ, "default=database"), "", "use softdeletes=none on the model", []correction{{field("DeletedAt", typ, "default=database"), "softdeletes=none"}}},
		)
	}
	managed := field("Created", "temporal.DateTime", "role=created-at") + field("Updated", "time.Time", "role=updated-at")
	soft := field("Deleted", "value.Nullable[time.Time]", "role=soft-delete")
	cases = append(cases,
		scenario{"renamed managed", managed + soft, "", "", nil},
		scenario{"ineligible managed partner", field("CreatedAt", "temporal.DateTime", "role=created-at") + field("UpdatedAt", "value.Nullable[time.Time]", ""), "", "use timestamps=none on the model", []correction{
			{field("CreatedAt", "temporal.DateTime", "") + field("UpdatedAt", "value.Nullable[time.Time]", ""), "timestamps=none"},
		}},
		scenario{"eligible managed partner", field("CreatedAt", "temporal.DateTime", "role=created-at") + field("UpdatedAt", "time.Time", ""), "", "declare both role=created-at and role=updated-at", []correction{
			{field("CreatedAt", "temporal.DateTime", "role=created-at") + field("UpdatedAt", "time.Time", "role=updated-at"), ""},
			{field("CreatedAt", "temporal.DateTime", "") + field("UpdatedAt", "time.Time", ""), "timestamps=none"},
		}},
		scenario{"input single", field("CreatedAt", "temporal.DateTime", "input=temporal.DateTime,mutate=mutateTime"), "", "declare role=time", []correction{
			{field("CreatedAt", "temporal.DateTime", "role=time,input=temporal.DateTime,mutate=mutateTime"), ""},
		}},
		scenario{"input pair", field("CreatedAt", "temporal.DateTime", "role=time,input=temporal.DateTime,mutate=mutateTime") + field("UpdatedAt", "time.Time", "role=time"), "", "use timestamps=none on the model", []correction{
			{field("CreatedAt", "temporal.DateTime", "input=temporal.DateTime,mutate=mutateTime") + field("UpdatedAt", "time.Time", ""), "timestamps=none"},
		}},
		scenario{"input deletion", field("DeletedAt", "value.Nullable[temporal.DateTime]", "role=time,input=temporal.DateTime,mutate=mutateTime"), "", "use softdeletes=none on the model", []correction{
			{field("DeletedAt", "value.Nullable[temporal.DateTime]", "input=temporal.DateTime,mutate=mutateTime"), "softdeletes=none"},
		}},
		scenario{"role displaces deletion name", field("DeletedAt", "temporal.DateTime", "role=created-at") + field("Updated", "time.Time", "role=updated-at"), "", "", nil},
		scenario{"role displaces creation name", field("CreatedAt", "value.Nullable[time.Time]", "role=soft-delete") + field("UpdatedAt", "temporal.DateTime", "role=time"), "", "", nil},
		scenario{"missing managed partner", field("CreatedAt", "temporal.DateTime", "role=created-at"), "", "declare both role=created-at and role=updated-at", []correction{
			{field("CreatedAt", "temporal.DateTime", "role=created-at") + field("UpdatedAt", "time.Time", "role=updated-at"), ""},
			{field("CreatedAt", "temporal.DateTime", ""), "timestamps=none"},
		}},
		scenario{"displaced ordinary names", managed + soft + field("CreatedAt", "temporal.DateTime", "role=time") + field("UpdatedAt", "temporal.DateTime", "role=time") + field("DeletedAt", "temporal.DateTime", "role=time"), "", "", nil},
		scenario{"displaced missing role", soft + field("DeletedAt", "temporal.DateTime", ""), "", "declare role=time", []correction{{soft + field("DeletedAt", "temporal.DateTime", "role=time"), ""}}},
		scenario{"ignored counterpart", field("CreatedAt", "temporal.DateTime", "role=time") + field("UpdatedAt", "temporal.DateTime", "-"), "", "", nil},
		scenario{"disabled conventions", field("CreatedAt", "temporal.DateTime", "") + field("UpdatedAt", "value.Nullable[time.Time]", "") + field("DeletedAt", "temporal.Date", ""), "timestamps=none softdeletes=none", "", nil},
		scenario{"redundant disabled time", field("CreatedAt", "temporal.DateTime", "role=time"), "timestamps=none", "role=time is inferred from the field type; remove it", []correction{{field("CreatedAt", "temporal.DateTime", ""), "timestamps=none"}}},
		scenario{"redundant unrelated time", field("PublishedAt", "temporal.DateTime", "role=time"), "", "role=time is inferred from the field type; remove it", []correction{{field("PublishedAt", "temporal.DateTime", ""), ""}}},
	)
	const imports = `package sample
import("time";"github.com/weiloon1234/Foundry-Go/temporal";"github.com/weiloon1234/Foundry-Go/value")
var _ time.Time
var _ temporal.DateTime
var _ value.Nullable[int]
func mutateTime(v temporal.DateTime)(temporal.DateTime,error){return v,nil}
`
	source := func(name, fields, options string) string {
		return "//foundry:model table=" + strings.ToLower(name) + " var=" + name + "s key=ID " + options + "\ntype " + name + " struct{ID int;" + fields + "}\n"
	}
	fixtures := newRejectionFixtures(t)
	var accepted strings.Builder
	accepted.WriteString(imports)
	var tables []string
	accept := func(f correction) {
		name := fmt.Sprintf("Case%d", len(tables))
		accepted.WriteString(source(name, f.fields, f.options))
		tables = append(tables, name+"s")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.message == "" {
				accept(correction{tc.fields, tc.options})
				return
			}
			dir := fixtures.fixture(t, imports+source("Account", tc.fields, tc.options))
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v; want %s", err, tc.message)
			}
			if strings.Contains(tc.message, "use timestamps=none") || strings.Contains(tc.message, "use softdeletes=none") {
				if strings.Contains(err.Error(), "role=time") {
					t.Fatalf("invalid ordinary role suggestion: %v", err)
				}
				for _, managed := range []string{"role=created-at", "role=soft-delete"} {
					if !strings.Contains(tc.message, managed) && strings.Contains(err.Error(), managed) {
						t.Fatalf("invalid managed role suggestion: %v", err)
					}
				}
			}
			for _, fix := range tc.fixes {
				accept(fix)
			}
		})
	}
	dir := fixture(t, accepted.String())
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "timestamps_test.go", `package sample
import("testing";"github.com/weiloon1234/Foundry-Go/database/orm")
func TestDeclarations(t *testing.T){if err:=orm.Check(`+strings.Join(tables, ",")+`);err!=nil{t.Fatal(err)}}`)
	cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-p", "4", "-run", "^TestDeclarations$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("ORM checked %d accepted declarations and suggested corrections", len(tables))
}
