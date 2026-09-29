package extensionstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_settings primary=Name
type Setting struct {
	Name        string
	Version     uint32
	Value       value.JSON[json.RawMessage]
	Kind        string
	Parameters  value.JSON[json.RawMessage]
	GroupName   string
	Label       string
	Description string
	SortOrder   int32
	IsPublic    bool
	// PresentationDeclared is true while the presentation follows the key's
	// declaration; Configure sets it false to keep an explicit presentation.
	PresentationDeclared bool
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func (Setting) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("setting row")) }

// SettingState selects version and presentation columns only, never values.
//
//foundry:projection
type SettingState struct {
	Name                 string
	Version              uint32
	Kind                 string
	Parameters           value.JSON[json.RawMessage]
	GroupName            string
	Label                string
	Description          string
	SortOrder            int32
	IsPublic             bool
	PresentationDeclared bool
}

func SettingStates(q SettingQuery) query.ProjectionQuery[Setting, SettingState] {
	f := SettingFields()
	return SelectSettingState(q, SettingStateSelection[Setting]{Name: f.Name.Value(), Version: f.Version.Value(), Kind: f.Kind.Value(), Parameters: f.Parameters.Value(), GroupName: f.GroupName.Value(), Label: f.Label.Value(), Description: f.Description.Value(), SortOrder: f.SortOrder.Value(), IsPublic: f.IsPublic.Value(), PresentationDeclared: f.PresentationDeclared.Value()})
}
