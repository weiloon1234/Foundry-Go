package model

// MorphName is the stored discriminator naming one model in polymorphic
// relationships. Store it in a model field of this type.
type MorphName string

// Morphable is implemented by a model that takes part in polymorphic
// relationships. Declaring the name once on the model is the typed morph map:
// relationship constructors read it from the model type, so call sites never
// pass discriminator strings.
//
//	func (Post) MorphName() model.MorphName { return "post" }
type Morphable interface{ MorphName() MorphName }
