package extensionstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_model_translations var=Translations key=Key identity=false
type Translation struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Translations.Query()).
	//
	// KeyColumn binds Translation.Key.
	// Example: orm.Set(Translations.KeyColumn, key).
	Key string `foundry:"member=KeyColumn"`
	// Owner binds Translation.Owner.
	// Example: orm.Set(Translations.Owner, owner).
	Owner string
	// Scope binds Translation.Scope.
	// Example: orm.Set(Translations.Scope, scope).
	Scope string
	// SubjectKey binds Translation.SubjectKey.
	// Example: orm.Set(Translations.SubjectKey, subjectKey).
	SubjectKey string
	// Identity binds Translation.Identity.
	// Example: encoded, err := value.NewJSON(identity); if err == nil { _ = orm.Set(Translations.Identity, encoded) }.
	Identity value.JSON[model.Identity]
	// Field binds Translation.Field.
	// Example: orm.Set(Translations.Field, field).
	Field string
	// Locale binds Translation.Locale.
	// Example: orm.Set(Translations.Locale, locale).
	Locale string
	// Value binds Translation.Value.
	// Example: orm.Set(Translations.Value, value).
	Value string
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	//
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Translations.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	//
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Translations.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
}

func (Translation) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model translation row")) }
