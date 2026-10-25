// Package cleanupfixture reads expectations captured from the legacy model API
// at 0ada5ac7 before its directives and owned outputs were removed.
package cleanupfixture

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/weiloon1234/Foundry-Go/extensions"
)

// Statement preserves exact SQL and parameter values. Example: snapshot.Statements["read"].SQL.
type Statement struct {
	// SQL is the exact compiled text. Example: snapshot.Statements["read"].SQL.
	SQL string
	// Arguments preserves parameter order and values. Example: snapshot.Statements["read"].Arguments.
	Arguments []any
}

// Snapshot retains independent identity, query and extension expectations.
// Example: snapshot := cleanupfixture.Read(t).
type Snapshot struct {
	// Identity is the persisted reference JSON. Example: json.Unmarshal(snapshot.Identity, &identity).
	Identity json.RawMessage
	// Statements contains named query expectations. Example: snapshot.Statements["read"].
	Statements map[string]Statement
	// Description preserves the full extension declaration. Example: snapshot.Description == fmt.Sprintf("%#v", declaration.Describe()).
	Description string
	// Subject retains the extension storage identity when present. Example: snapshot.Subject.Scope.
	Subject *extensions.Subject
}

// Read loads the calling package's frozen legacy expectations. Example: Read(t).
func Read(t testing.TB) Snapshot {
	t.Helper()
	data, err := os.ReadFile("testdata/legacy-model.json")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
