package main

import (
	"fmt"
	"regexp"

	"github.com/weiloon1234/Foundry-Go/internal/docsource"
)

// query.X is already rejected by retirement.CheckDocument. These remaining
// generated names depend on application model names, not the retirement ledger.
var generatedModelTeaching = regexp.MustCompile(`foundry:(model|projection)|\b(?:models|reports)\.\w+Fields(?:At)?\(|\b\w+Draft\{|\bmodels\.Query\w+\(`)

func checkModelGuide(name string, data []byte) error {
	if !docsource.RequiresCompiledSource(name) {
		return nil
	}
	doc := string(data)
	if generatedModelTeaching.MatchString(doc) {
		return fmt.Errorf("%s: guide teaches a retired generated model/query API", name)
	}
	return nil
}
