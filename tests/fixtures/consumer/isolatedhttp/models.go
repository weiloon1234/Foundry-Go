// Package isolatedhttp exercises production assembly in test-owned schemas.
package isolatedhttp

import "github.com/weiloon1234/Foundry-Go/model"

//foundry:model table=scope_records var=RecordRows key=ID identity=false
type Record struct {
	// Table owns the primary key. Example: orm.Eq(RecordRows.Key(), id).
	ID model.ID[Record]
	// Name binds the Name field. Example: orm.Set(RecordRows.Name, "fixture").
	Name string
	// Text binds the Text field. Example: orm.Set(RecordRows.Text, "text").
	Text string
}

//foundry:dto http=body
type Submission struct {
	Name     string `json:"name"`
	Text     string `json:"text"`
	Rollback bool   `json:"rollback"`
}

//foundry:dto http=response
type Receipt struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

//foundry:path pattern=/records/{name}
type RecordPath struct{ Name string }
