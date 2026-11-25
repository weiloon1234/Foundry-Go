package guideadapters_test

import (
	"testing"

	ext "foundry.test/goway/models/extensions"
)

func TestSlotRecipesDeclareValidCollections(t *testing.T) {
	if err := ext.Photos.Validate(); err != nil {
		t.Fatalf("Photos declaration is invalid: %v", err)
	}
	if _, err := ext.QueuedPhotos(nil); err == nil {
		t.Fatal("QueuedPhotos accepted a missing outbox producer")
	}
	if err := ext.RenamedOwner.Validate(); err != nil {
		t.Fatalf("RenamedOwner is invalid: %v", err)
	}
	if err := ext.RecoveredOwner.Validate(); err != nil {
		t.Fatalf("RecoveredOwner is invalid: %v", err)
	}
}
