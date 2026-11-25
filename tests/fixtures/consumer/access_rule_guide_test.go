package consumer_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The authentication guide quotes the compiler's refusal of each access-rule
// mistake. The text must be what the compiler prints now, so a renamed type or a
// changed signature cannot leave the guide teaching a message that no longer exists.
func TestGuideQuotesWhatTheCompilerPrintsForAccessRuleMistakes(t *testing.T) {
	guide, err := os.ReadFile("../../../docs/guides/authentication.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"access_rule_omitted", "access_rule_flat_scopes", "access_rule_configure_scopes"} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "go", "build", "-o", os.DevNull, "./testdata/compilefail/"+name)
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			if cmd.Run() == nil {
				t.Fatal("the invalid contract compiled")
			}
			var message []string
			for _, line := range strings.Split(out.String(), "\n") {
				switch match := compilerDiagnosticLine.FindStringSubmatch(line); {
				case len(match) != 0:
					message = append(message, match[2])
				case len(message) > 0 && strings.HasPrefix(line, "\t"):
					message = append(message, line)
				}
			}
			if len(message) == 0 {
				t.Fatal("no diagnostic in the compiler output:", out.String())
			}
			if quoted := strings.Join(message, "\n"); !strings.Contains(string(guide), quoted) {
				t.Fatalf("docs/guides/authentication.md does not quote what the compiler prints:\n%s", quoted)
			}
		})
	}
}
