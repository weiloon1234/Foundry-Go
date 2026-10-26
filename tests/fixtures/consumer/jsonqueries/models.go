package jsonqueries

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Preferences is the single declaration of the persisted JSON payload.
type Preferences struct {
	// Theme chooses the reader appearance; omitted uses the application default.
	Theme   value.Optional[string]            `json:"theme,omitzero"`
	Labels  value.Optional[map[string]string] `json:"labels,omitzero"`
	Quota   value.Optional[decimal.Decimal]   `json:"quota,omitzero"`
	Profile value.Optional[Profile]           `json:"profile,omitzero"`
}

// Profile exercises nested, recursive and differently encoded payload fields.
type Profile struct {
	Name       string         `json:"name"`
	Age        int64          `json:"age,string"`
	Nickname   *string        `json:"nickname"`
	Scores     []int64        `json:"scores"`
	Attributes map[int]string `json:"attributes"`
	Next       *Profile       `json:"next"`
	Quoted     string         `json:"quoted,string"`
	Odd        string         `json:"odd%path"`
}

type PolicyKey struct {
	Version int64  `json:"version"`
	Region  string `json:"region"`
}

type Document struct {
	ID       int
	Settings value.JSON[Preferences]
	Backup   value.Nullable[value.JSON[Preferences]]
	Tags     value.JSON[[]string]
	State    value.JSON[value.Nullable[string]]
	PolicyID value.JSON[PolicyKey]
	Policy   relation.One[Policy]
}

type Policy struct {
	ID   value.JSON[PolicyKey]
	Name string
}

type Snapshot struct {
	ID         int
	Settings   value.JSON[Preferences]
	Kind       database.JSONKind
	BackupKind value.Nullable[database.JSONKind]
	Matches    value.Nullable[bool]
}
