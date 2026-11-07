package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/docsource"
)

// query.X is already rejected by retirement.CheckDocument. These remaining
// generated names depend on application model names, not the retirement ledger.
var generatedModelTeaching = regexp.MustCompile(`foundry:(model|projection)|\b(?:models|reports)\.\w+Fields(?:At)?\(|\b\w+Draft\{|\bmodels\.Query\w+\(`)

func checkMigrationGuide(name string, data []byte) error {
	if !docsource.RequiresORMMigration(name) {
		return nil
	}
	var failures []error
	doc := string(data)
	if generatedModelTeaching.MatchString(doc) {
		failures = append(failures, fmt.Errorf("%s: guide still teaches a generated model/query API; link orm-migration.md instead", name))
	}
	if !strings.Contains(doc, "(orm-migration.md)") {
		failures = append(failures, fmt.Errorf("%s: guide needs the declaration migration link", name))
	}
	return errors.Join(failures...)
}
