package contract

import "testing"

func TestMainGenericIdentityOnlySubstitutesSourceNamespaces(t *testing.T) {
	for _, test := range []struct {
		runtime, source string
		want            bool
	}{
		{"main.Request", "app.test/project.Request", true},
		{"f.Token[main.Member,f.Reset]", "f.Token[app.test/project.Member,f.Reset]", true},
		{"f.Token[f.Wrap[main.Member],f.Reset]", "f.Token[f.Wrap[app.test/project.Member],f.Reset]", true},
		{"f.Token[main.Member,f.Reset]", "g.Token[app.test/project.Member,f.Reset]", false},
		{"f.Token[main.Member,f.Reset]", "f.Token[app.test/project.Admin,f.Reset]", false},
		{"f.Token[main.Member,f.Reset]", "f.Token[app.test/project.Member,f.Verify]", false},
		{"f.Token[main.Member,f.Reset]", "f.Token[app.test/project.Member]", false},
		{"f.Token[main.Member,f.Reset]", "f.Token[app.test/project.Member,,f.Reset]", false},
		{"f.Token[main.Member,f.Reset]", "f.Token[app.test/project.Member,f.Reset", false},
		{"f.Token[app.Member,f.Reset]", "f.Token[other.Member,f.Reset]", false},
		{"f.Token[main.Member,f.Reset]", "f.Token[evil path.Member,f.Reset]", false},
	} {
		if got := mainTypeIdentityMatches(test.runtime, test.source); got != test.want {
			t.Errorf("identity %q -> %q = %v", test.runtime, test.source, got)
		}
	}
}
