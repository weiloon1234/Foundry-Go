package modelconvert

import (
	"strings"
	"testing"
)

func TestMemberDocumentationMovesToModel(t *testing.T) {
	for _, existing := range []string{"", "// Domain field documentation.\n"} {
		t.Run(existing, func(t *testing.T) {
			source := strings.Replace(string(read(t, "plain.before")), " Name string", existing+" Name string", 1)
			source = strings.Replace(source, " Name orm.Text", " // Member behavior documentation.\n Name orm.Text", 1)
			plan := analyze(t, fixture(t, source))
			if len(plan.Refused) != 0 {
				t.Fatal(plan.Refused)
			}
			if len(plan.Models) != 1 || len(plan.Models[0].Documentation) != 1 || plan.Models[0].Documentation[0].Appended != (existing != "") {
				t.Fatalf("documentation report: %+v", plan.Models)
			}
			got := string(plan.Files[0].After)
			if !strings.Contains(got, "// Member behavior documentation.\n\tName string") {
				t.Fatalf("member documentation lost:\n%s", got)
			}
			if existing != "" && !strings.Contains(got, "// Domain field documentation.\n\t//\n\t// Member behavior documentation.") {
				t.Fatalf("field documentation precedence lost:\n%s", got)
			}
		})
	}
}

func TestMemberDocumentationRetainsDistinctText(t *testing.T) {
	for _, existing := range []string{"Member documentation.", "Longer Member documentation. More context."} {
		t.Run(existing, func(t *testing.T) {
			source := strings.Replace(string(read(t, "plain.before")), " Name string", " // "+existing+"\n Name string", 1)
			source = strings.Replace(source, " Name orm.Text[User,string]", " Name orm.Text[User,string] // Member documentation.", 1)
			plan := analyze(t, fixture(t, source))
			if len(plan.Refused) != 0 {
				t.Fatal(plan.Refused)
			}
			got := string(plan.Files[0].After)
			want := 1
			if existing != "Member documentation." {
				want = 2
			}
			if strings.Count(got, "Member documentation.") != want {
				t.Fatalf("documentation merge:\n%s", got)
			}
		})
	}
}
