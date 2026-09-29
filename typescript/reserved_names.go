package typescript

import (
	"regexp"
	"sync"
)

// renderedNames are module-level declarations the renderer itself emits.
var renderedNames = []string{"API", "ContractTypes", "ErrorResponse", "Identity", "Operations", "Realtime", "ReceivedContractTypes"}

var capitalizedIdentifier = regexp.MustCompile(`\b[A-Z][A-Za-z0-9_$]*\b`)

// reservedNames holds every capitalized identifier of the generated module's
// fixed text: runtime declarations and the global types they use. A schema with
// one of these names would shadow it, so it receives a qualified name instead.
// Words from comments and strings are included; over-reserving only qualifies
// a schema name.
var reservedNames = sync.OnceValues(func() (map[string]bool, error) {
	result := make(map[string]bool)
	for _, name := range renderedNames {
		result[name] = true
	}
	entries, err := runtimeSources.ReadDir("runtime")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := runtimeSources.ReadFile("runtime/" + entry.Name())
		if err != nil {
			return nil, err
		}
		for _, name := range capitalizedIdentifier.FindAll(data, -1) {
			result[string(name)] = true
		}
	}
	return result, nil
})
