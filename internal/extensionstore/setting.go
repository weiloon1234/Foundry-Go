package extensionstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_settings var=Settings key=Name identity=false
type Setting struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Settings.Query()).
	//
	// Name binds Setting.Name.
	// Example: orm.Set(Settings.Name, name).
	Name string `foundry:"member=Name"`
	// Version binds Setting.Version.
	// Example: orm.Set(Settings.Version, version).
	Version uint32
	// Value binds Setting.Value.
	// Example: encoded, err := value.NewJSON(value); if err == nil { _ = orm.Set(Settings.Value, encoded) }.
	Value value.JSON[json.RawMessage]
	// Kind binds Setting.Kind.
	// Example: orm.Set(Settings.Kind, kind).
	Kind string
	// Parameters binds Setting.Parameters.
	// Example: encoded, err := value.NewJSON(parameters); if err == nil { _ = orm.Set(Settings.Parameters, encoded) }.
	Parameters value.JSON[json.RawMessage]
	// GroupName binds Setting.GroupName.
	// Example: orm.Set(Settings.GroupName, groupName).
	GroupName string
	// Label binds Setting.Label.
	// Example: orm.Set(Settings.Label, label).
	Label string
	// Description binds Setting.Description.
	// Example: orm.Set(Settings.Description, description).
	Description string
	// SortOrder binds Setting.SortOrder.
	// Example: orm.Set(Settings.SortOrder, sortOrder).
	SortOrder int32
	// IsPublic binds Setting.IsPublic.
	// Example: orm.Set(Settings.IsPublic, isPublic).
	IsPublic bool `foundry:"role=column"`
	// PresentationDeclared is true while the presentation follows the key's
	// declaration; Configure sets it false to keep an explicit presentation.
	//
	// PresentationDeclared binds Setting.PresentationDeclared.
	// Example: orm.Set(Settings.PresentationDeclared, presentationDeclared).
	PresentationDeclared bool `foundry:"role=column"`
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	//
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Settings.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	//
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Settings.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
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
