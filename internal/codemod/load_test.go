package codemod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryLoadsConsumerDependencyAndTestVariants(t *testing.T) {
	root := t.TempDir()
	dependency := filepath.Join(root, "dependency")
	consumer := filepath.Join(root, "consumer")
	// The repository owns the Go requirement, including synthetic modules.
	module, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	var goDirective string
	for _, line := range strings.Split(string(module), "\n") {
		if strings.HasPrefix(line, "go ") {
			goDirective = line
			break
		}
	}
	if goDirective == "" {
		t.Fatal("missing repository Go requirement")
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dependency, "go.mod"), "module "+strings.TrimSuffix(validationPath, "/validation")+"\n"+goDirective+"\n")
	write(filepath.Join(dependency, "validation/validation.go"), oldValidation)
	write(filepath.Join(dependency, "http/http.go"), oldHTTP)
	write(filepath.Join(consumer, "go.mod"), "module consumer\n"+goDirective+"\nrequire "+strings.TrimSuffix(validationPath, "/validation")+" v0.0.0\nreplace "+strings.TrimSuffix(validationPath, "/validation")+" => ../dependency\n")
	write(filepath.Join(consumer, "types.go"), "package consumer\nimport v \""+validationPath+"\"\nvar rule v.Rule[int]\n")
	// This file deliberately has no validation import: the rule adds one.
	source := "package consumer\nvar _ = rule.Validate()\n"
	write(filepath.Join(consumer, "use.go"), source)
	write(filepath.Join(consumer, "use_test.go"), "package consumer\nvar _ = rule.Validate()\n")
	write(filepath.Join(consumer, "dto_foundry.gen.go"), "package consumer\nvar _ = rule.Validate()\n")
	// The CLI passes Validation explicitly; Inventory must also register HTTP.
	write(filepath.Join(consumer, "http_types.go"), "package consumer\nimport h \""+httpPath+"\"\nvar endpoint h.Endpoint[int,int,int,int]\n")
	write(filepath.Join(consumer, "http_use.go"), "package consumer\nvar _ = endpoint.Validate()\n")

	t.Setenv("GOWORK", "off")
	plan, err := Inventory(t.Context(), Options{Dir: consumer}, Validation{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 3 || len(plan.Entries) != 4 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	original, err := os.ReadFile(filepath.Join(consumer, "use.go"))
	if err != nil || string(original) != source {
		t.Fatal("dry run mutated source", err)
	}
	for _, change := range plan.Changes {
		if !strings.Contains(string(change.After), "foundryvalidation.ValidateRule(rule)") && !strings.Contains(string(change.After), "foundryhttp.ValidateEndpoint(endpoint)") {
			t.Fatalf("missing import/rewrite: %s", change.After)
		}
	}
	if count, err := plan.Apply(); err != nil || count != 3 {
		t.Fatalf("apply: %d %v", count, err)
	}
}
