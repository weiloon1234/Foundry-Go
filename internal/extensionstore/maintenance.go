package extensionstore

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Maintenance selects ownership/index columns only, never extension payloads.
type MetaIndex struct {
	Key        string
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Name       string
	Version    uint32
}

type TranslationIndex struct {
	Key        string
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Field      string
	Locale     string
}
