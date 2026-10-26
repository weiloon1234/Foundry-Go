package generate

import "fmt"

// Slot metadata belongs to the native ORM model scaffold.
const (
	textSlot  = "Text"
	oneSlot   = "One"
	manySlot  = "Many"
	valueSlot = "Value"
)

var reservedExtensionSlotNames = map[string]bool{"From": true, "All": true}

type extensionSlot struct{ name, stored, kind string }

// extensionStorage names the framework table each slot kind is stored in.
var extensionStorage = map[string]string{
	textSlot:  "foundry_model_translations",
	oneSlot:   "foundry_attachments",
	manySlot:  "foundry_attachments",
	valueSlot: "foundry_model_metadata",
}

// extensionSlotDescription is the shared one-line summary for generated
// documentation and handwritten field notices.
func extensionSlotDescription(slot extensionSlot) string {
	switch slot.kind {
	case textSlot:
		return fmt.Sprintf("translated text stored in %s as field %q, one row per locale", extensionStorage[slot.kind], slot.stored)
	case oneSlot:
		return fmt.Sprintf("the single file of attachment collection %q, recorded in %s with its bytes on the policy's disk", slot.stored, extensionStorage[slot.kind])
	case manySlot:
		return fmt.Sprintf("the ordered files of attachment collection %q, recorded in %s with their bytes on the policy's disk", slot.stored, extensionStorage[slot.kind])
	}
	return fmt.Sprintf("the typed metadata value %q stored in %s, with no model column", slot.stored, extensionStorage[slot.kind])
}
