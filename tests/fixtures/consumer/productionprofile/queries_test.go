package productionprofile_test

import (
	"foundry.test/consumer/productionprofile"
	"testing"
)

func TestProfileQueryShapes(t *testing.T) {
	if err := productionprofile.Check(); err != nil {
		t.Fatal(err)
	}
}
