package oracle

import (
	"strings"
	"testing"
)

func TestFreezeEnvironmentRejectsUntrackedBuildInputs(t *testing.T) {
	for _, input := range []string{"GOFLAGS=-gcflags=all=-l", "GOFLAGS=-overlay=/elsewhere.json", "GOWORK=/external/go.work"} {
		if _, err := FixedEnvironment([]string{input}); err == nil {
			t.Fatal(input)
		}
	}
	env, err := FixedEnvironment([]string{"GOFLAGS=-p=4", "GOWORK=off", "GOENV=/external/settings", "GOARM64=v9.0", "GOCACHE=/task/cache"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "/external") || strings.Contains(joined, "GOARM64=") || !strings.Contains(joined, "GOCACHE=/task/cache") {
		t.Fatal(env)
	}
	b := sample()
	for id, r := range b.Records {
		r.Provenance.Build.GOFLAGS = "-gcflags=all=-l"
		b.Records[id] = r
	}
	if Validate(b) == nil {
		t.Fatal("unfingerprinted gcflags accepted")
	}
}
