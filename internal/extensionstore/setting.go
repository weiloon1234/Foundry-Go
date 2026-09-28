package extensionstore

import (
	"encoding/json"
	"fmt"

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
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func (Setting) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("setting row")) }
