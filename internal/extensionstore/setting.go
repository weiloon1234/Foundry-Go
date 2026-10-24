package extensionstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

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
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	CreatedAt temporal.DateTime
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	UpdatedAt temporal.DateTime
}

func (Setting) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("setting row")) }

// SettingState selects version and presentation columns only, never values.
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
