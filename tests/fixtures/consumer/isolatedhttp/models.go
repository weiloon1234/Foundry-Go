// Package isolatedhttp exercises production assembly in retained test schemas.
package isolatedhttp

import "github.com/weiloon1234/Foundry-Go/model"

//foundry:model table=scope_records
type Record struct {
	ID   model.ID[Record]
	Name string
	Text string
}

//foundry:dto
type Submission struct {
	Name     string `json:"name"`
	Text     string `json:"text"`
	Rollback bool   `json:"rollback"`
}

//foundry:dto
type Receipt struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

//foundry:path pattern=/records/{name}
type RecordPath struct{ Name string }
