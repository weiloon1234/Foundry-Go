package oracle

import (
	"os"
	"path/filepath"

	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Capture/replay overrides select observations, not the reviewed source census.
func (s Selection) currentEnvironment(prefix string) (Selection, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Selection{}, err
	}
	dir, err := directoryAt(cwd)
	if err != nil {
		return Selection{}, err
	}
	return s.forEnvironment(dir, prefix)
}

// The reviewed capture census owns backend requirements. Test names and whether
// any observation happened are not evidence that a test requires PostgreSQL.
func (s Selection) forEnvironment(dir, prefix string) (Selection, error) {
	if os.Getenv(postgres.URLVariable) != "" || os.Getenv(postgres.RequiredVariable) == "1" {
		return s, nil
	}
	var audit ComparisonAudit
	if err := ReadJSON(filepath.Join(dir, "comparisons.json"), &audit); err != nil {
		return Selection{}, err
	}
	for file, source := range audit.Files {
		if filepath.ToSlash(filepath.Dir(file)) == prefix {
			s.disabled = append(s.disabled, source.PostgresTests...)
		}
	}
	return s, nil
}
